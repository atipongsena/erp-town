package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oklog/ulid/v2"

	"github.com/atipongsena/erp-town/go/internal/events/gen"
	"github.com/atipongsena/erp-town/go/internal/store/sqlc"
)

// town_outbox statuses.
const (
	StatusPending   = "Pending"
	StatusPublished = "Published"
	StatusFailed    = "Failed"
	StatusInvalid   = "Invalid"
)

// system.error codes raised by the outbox.
const (
	EventInvalid        = "EVENT_INVALID"
	OutboxPublishFailed = "OUTBOX_PUBLISH_FAILED"
)

const (
	// TypeSystemError is the event type of town-layer errors.
	TypeSystemError = "system.error"
	// SystemErrorSource is the envelope source of system.error events raised by the Go
	// outbox (the Frappe outbox uses "erp/town").
	SystemErrorSource = "town/outbox"
	// OutboxDocType is the subject doctype of publisher-level errors (subject name = the
	// outbox row id), the same doctype name the Frappe outbox uses.
	OutboxDocType = "Town Event Outbox"

	maxErrorLength = 1000
)

// systemErrorRoles may see system.error events.
var systemErrorRoles = []string{"System Manager"}

// SystemActor is the actor of events the town layer raises on its own.
func SystemActor() Actor { return Actor{Kind: "system", ID: "system.town"} }

var (
	// ErrERPOwnedType is returned for erp.* and workflow.* events: ERPNext owns those
	// subjects' seq, so Go never emits them. Nothing is stored.
	ErrERPOwnedType = errors.New("events: erp.* and workflow.* events are emitted by ERPNext only")
	// ErrEventInvalid is returned when the event does not match the contract. The event
	// is stored as an Invalid row (never published) and reported as system.error
	// EVENT_INVALID; Emit returns the Invalid row's id along with this error.
	ErrEventInvalid = errors.New("events: event does not match the contract")
)

// EmitInput is one event to emit; Emit adds specversion, id, time, schema_version and seq.
// Data is encoded as JSON without HTML escaping.
type EmitInput struct {
	Type, Source  string
	Company       *string
	Actor         Actor
	Subject       Subject
	CorrelationID string
	CausationID   *string
	Visibility    Visibility
	Data          any
}

// Emit stores an event in town_outbox inside the caller's transaction tx and returns its
// id; the publisher sends it to the stream once tx has committed. It allocates the
// subject's next seq (rolled back with tx), a new ULID and the envelope time
// FormatBangkok(now), validates exactly the bytes it stores and stores them Pending.
//
// erp.* and workflow.* types fail with ErrERPOwnedType. An event that does not match
// the contract still takes its seq and is stored Invalid with the reason in last_error,
// a Pending system.error EVENT_INVALID (causation_id = the invalid event's id) is
// emitted in tx, and Emit returns the invalid event's id with ErrEventInvalid. Database
// errors are returned as is; tx is then unusable and the caller must roll it back.
func Emit(ctx context.Context, tx pgx.Tx, v *Validator, now time.Time, in EmitInput) (string, error) {
	if strings.HasPrefix(in.Type, "erp.") || strings.HasPrefix(in.Type, "workflow.") {
		return "", fmt.Errorf("%w: %s", ErrERPOwnedType, in.Type)
	}
	id, reason, err := store(ctx, tx, v, now, in)
	if err != nil {
		return "", err
	}
	if reason == "" {
		return id, nil
	}
	subject := in.Subject
	if subject.DocType == "" || subject.Name == "" {
		subject = Subject{DocType: OutboxDocType, Name: id}
	}
	report := systemErrorEvent(EventInvalid, fmt.Sprintf("event %s (%s) is invalid: %s", id, in.Type, reason), subject, id)
	rid, rreason, err := store(ctx, tx, v, now, report)
	if err != nil {
		return "", fmt.Errorf("report invalid event %s: %w", id, err)
	}
	if rreason != "" {
		// Never report a report: that could not end. The row stays Invalid for inspection.
		slog.Error("outbox: EVENT_INVALID report is itself invalid", "event_id", id, "report_id", rid, "reason", rreason)
	}
	return id, fmt.Errorf("%w: %s", ErrEventInvalid, reason)
}

// store allocates seq and id, builds and validates the payload and inserts the row:
// Pending, or Invalid when reason is not empty.
func store(ctx context.Context, tx pgx.Tx, v *Validator, now time.Time, in EmitInput) (id, reason string, err error) {
	q := sqlc.New(tx)
	seq, err := q.NextSubjectSeq(ctx, sqlc.NextSubjectSeqParams{SubjectDoctype: in.Subject.DocType, SubjectName: in.Subject.Name})
	if err != nil {
		return "", "", fmt.Errorf("outbox: allocate seq: %w", err)
	}
	uid, err := ulid.New(ulid.Timestamp(now), ulid.DefaultEntropy())
	if err != nil {
		return "", "", fmt.Errorf("outbox: new event id: %w", err)
	}
	id = uid.String()
	payload, reason, err := buildPayload(v, id, seq, now, in)
	if err != nil {
		return "", "", err
	}
	status, lastError := StatusPending, pgtype.Text{}
	if reason != "" {
		status, lastError = StatusInvalid, pgtype.Text{String: truncate(reason, maxErrorLength), Valid: true}
	}
	if err := q.InsertOutbox(ctx, sqlc.InsertOutboxParams{
		ID: id, Type: in.Type, SubjectDoctype: in.Subject.DocType, SubjectName: in.Subject.Name,
		Payload: string(payload), Status: status, LastError: lastError, CreatedAt: timestamptz(now),
	}); err != nil {
		return "", "", fmt.Errorf("outbox: insert event %s: %w", id, err)
	}
	return id, reason, nil
}

// buildPayload encodes the envelope and validates exactly those bytes. reason says why
// the event is invalid ("" when it is valid).
func buildPayload(v *Validator, id string, seq int64, now time.Time, in EmitInput) (payload []byte, reason string, err error) {
	data, err := marshalNoEscape(in.Data)
	if err != nil {
		reason, data = "data is not JSON-serializable: "+err.Error(), nil
	}
	payload, err = Marshal(Envelope{
		SpecVersion: "1.0", ID: id, Type: in.Type, Source: in.Source, Time: FormatBangkok(now),
		SchemaVersion: 1, Company: in.Company, Actor: in.Actor, Subject: in.Subject, Seq: seq,
		CorrelationID: in.CorrelationID, CausationID: in.CausationID, Visibility: in.Visibility, Data: data,
	})
	if err != nil {
		return nil, "", fmt.Errorf("outbox: encode event %s: %w", id, err)
	}
	if reason != "" {
		return payload, reason, nil
	}
	if err := v.Validate(payload); err != nil {
		var se *SchemaError
		if errors.As(err, &se) {
			return payload, strings.Join(se.Reasons, "; "), nil
		}
		return payload, err.Error(), nil
	}
	return payload, "", nil
}

// systemErrorEvent builds a system.error raised by the outbox itself: system actor,
// visible to System Manager only, no company, correlated by the subject's name (as the
// Frappe outbox does).
func systemErrorEvent(code, message string, subject Subject, causationID string) EmitInput {
	message = truncate(strings.TrimSpace(message), maxErrorLength)
	if message == "" {
		message = code
	}
	return EmitInput{
		Type:          TypeSystemError,
		Source:        SystemErrorSource,
		Actor:         SystemActor(),
		Subject:       subject,
		CorrelationID: subject.Name,
		CausationID:   &causationID,
		Visibility:    Visibility{Level: "roles", Roles: append([]string(nil), systemErrorRoles...)},
		Data:          gen.SystemErrorData{Code: code, Message: message},
	}
}

// truncate shortens s to at most n runes.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
