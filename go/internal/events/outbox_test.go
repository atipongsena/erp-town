package events

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/events/gen"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
)

var ulidPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// testNow is a fixed UTC instant (10:04:05 in Bangkok), at microsecond precision so it
// round-trips through timestamptz unchanged.
var testNow = time.Date(2026, 10, 9, 3, 4, 5, 123456000, time.UTC)

// systemErrorInput is a valid system.error about the given subject name.
func systemErrorInput(name string, data any) EmitInput {
	return EmitInput{
		Type:          "system.error",
		Source:        SystemErrorSource,
		Actor:         SystemActor(),
		Subject:       Subject{DocType: "Town Event Outbox", Name: name},
		CorrelationID: name,
		Visibility:    Visibility{Level: "roles", Roles: []string{"System Manager"}},
		Data:          data,
	}
}

// inTx runs fn in a transaction on pool and commits it.
func inTx(t *testing.T, pool *pgxpool.Pool, fn func(tx pgx.Tx)) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	fn(tx)
	require.NoError(t, tx.Commit(t.Context()))
}

// emitCommitted emits in its own committed transaction and returns the id.
func emitCommitted(t *testing.T, pool *pgxpool.Pool, v *Validator, now time.Time, in EmitInput) string {
	t.Helper()
	var id string
	inTx(t, pool, func(tx pgx.Tx) {
		var err error
		id, err = Emit(t.Context(), tx, v, now, in)
		require.NoError(t, err)
	})
	return id
}

type outboxRow struct {
	ID, Type, Status, Payload string
	Attempts                  int
	LastError, StreamID       *string
	NextAttemptAt             *time.Time
	PublishedAt               *time.Time
}

func loadRows(t *testing.T, pool *pgxpool.Pool) []outboxRow {
	t.Helper()
	rows, err := pool.Query(t.Context(), `
		SELECT id, type, status, payload, attempts, last_error, stream_id, next_attempt_at, published_at
		FROM town_outbox ORDER BY insert_order`)
	require.NoError(t, err)
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (outboxRow, error) {
		var o outboxRow
		err := r.Scan(&o.ID, &o.Type, &o.Status, &o.Payload, &o.Attempts, &o.LastError, &o.StreamID, &o.NextAttemptAt, &o.PublishedAt)
		return o, err
	})
	require.NoError(t, err)
	return out
}

func loadRow(t *testing.T, pool *pgxpool.Pool, id string) outboxRow {
	t.Helper()
	for _, r := range loadRows(t, pool) {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("outbox row %s not found", id)
	return outboxRow{}
}

// decodeStored validates a stored payload against the contract and decodes it.
func decodeStored(t *testing.T, v *Validator, payload string) Envelope {
	t.Helper()
	e, err := Decode(v, []byte(payload))
	require.NoError(t, err)
	return e
}

func TestFormatBangkok(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   time.Time
		want string
	}{
		{"utc", time.Date(2026, 10, 9, 3, 4, 5, 999, time.UTC), "2026-10-09T10:04:05+07:00"},
		{"crosses midnight", time.Date(2026, 12, 31, 17, 30, 0, 0, time.UTC), "2027-01-01T00:30:00+07:00"},
		{"other zone", time.Date(2026, 10, 9, 5, 4, 5, 0, time.FixedZone("CEST", 2*3600)), "2026-10-09T10:04:05+07:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, FormatBangkok(tc.in))
		})
	}
}

func TestEmitCannotAllocateERPSubjectSequenceForGoEvent(t *testing.T) {
	pool := storetest.NewPool(t)
	v, err := NewValidator()
	require.NoError(t, err)
	for _, dt := range []string{"Sales Order", "Delivery Note", "Sales Invoice", "Payment Entry", "Town Avatar"} {
		tx, err := pool.Begin(t.Context())
		require.NoError(t, err)
		in := systemErrorInput("DOC-1", map[string]string{"code": "EVENT_INVALID", "message": "Rejected."})
		in.Subject.DocType = dt
		_, err = Emit(t.Context(), tx, v, testNow, in)
		require.ErrorIs(t, err, ErrERPOwnedSubject)
		require.NoError(t, tx.Commit(t.Context()))
	}
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM town_subject_seq").Scan(&n))
	assert.Zero(t, n)
}

func TestEmitAllocatesSeqULIDAndBangkokTime(t *testing.T) {
	pool := storetest.NewPool(t)
	v := newValidator(t)
	in := systemErrorInput("01J9Z0000000000000000000AA", gen.SystemErrorData{Code: "TEST", Message: "hello"})

	first := emitCommitted(t, pool, v, testNow, in)
	second := emitCommitted(t, pool, v, testNow, in)

	rows := loadRows(t, pool)
	require.Len(t, rows, 2)
	for i, want := range []struct {
		id  string
		seq int64
	}{{first, 1}, {second, 2}} {
		r := rows[i]
		assert.Equal(t, want.id, r.ID)
		assert.Regexp(t, ulidPattern, r.ID)
		assert.Equal(t, "Pending", r.Status)
		assert.Equal(t, "system.error", r.Type)
		assert.Equal(t, 0, r.Attempts)
		assert.Nil(t, r.LastError)
		e := decodeStored(t, v, r.Payload)
		assert.Equal(t, r.ID, e.ID)
		assert.Equal(t, want.seq, e.Seq)
		assert.Equal(t, "2026-10-09T10:04:05+07:00", e.Time)
		assert.Equal(t, "1.0", e.SpecVersion)
		assert.Equal(t, 1, e.SchemaVersion)
		assert.Nil(t, e.Company)
		assert.JSONEq(t, `{"code":"TEST","message":"hello"}`, string(e.Data))
	}
	assert.NotEqual(t, first, second)
}

func TestEmitRefusesERPOwnedTypes(t *testing.T) {
	pool := storetest.NewPool(t)
	v := newValidator(t)
	for _, typ := range []string{"erp.sales_order.submitted", "workflow.approval.requested"} {
		t.Run(typ, func(t *testing.T) {
			in := systemErrorInput("SO-0001", gen.SystemErrorData{Code: "TEST", Message: "x"})
			in.Type = typ
			inTx(t, pool, func(tx pgx.Tx) {
				id, err := Emit(t.Context(), tx, v, testNow, in)
				require.ErrorIs(t, err, ErrERPOwnedType)
				assert.Empty(t, id)
			})
		})
	}
	assert.Empty(t, loadRows(t, pool))
	var seqs int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM town_subject_seq").Scan(&seqs))
	assert.Zero(t, seqs, "a refused event must not take a seq")
}

func TestEmitInvalidStoresInvalidRowAndReports(t *testing.T) {
	pool := storetest.NewPool(t)
	v := newValidator(t)
	in := systemErrorInput("01J9Z0000000000000000000AB", map[string]any{"message": "no code"})

	var id string
	inTx(t, pool, func(tx pgx.Tx) {
		var err error
		id, err = Emit(t.Context(), tx, v, testNow, in)
		require.ErrorIs(t, err, ErrEventInvalid)
	})
	require.Regexp(t, ulidPattern, id)

	rows := loadRows(t, pool)
	require.Len(t, rows, 2)
	invalid := loadRow(t, pool, id)
	assert.Equal(t, "Invalid", invalid.Status)
	require.NotNil(t, invalid.LastError)
	assert.Contains(t, *invalid.LastError, "code")
	assert.Nil(t, invalid.NextAttemptAt)

	var report outboxRow
	for _, r := range rows {
		if r.ID != id {
			report = r
		}
	}
	assert.Equal(t, "Pending", report.Status)
	assert.Equal(t, "system.error", report.Type)
	e := decodeStored(t, v, report.Payload)
	data, err := DecodeData[gen.SystemErrorData](e)
	require.NoError(t, err)
	assert.Equal(t, EventInvalid, data.Code)
	assert.Contains(t, data.Message, id)
	require.NotNil(t, e.CausationID)
	assert.Equal(t, id, *e.CausationID)
	assert.Equal(t, in.Subject, e.Subject, "EVENT_INVALID is about the invalid event's subject")
	assert.Equal(t, int64(2), e.Seq, "the invalid event took seq 1 of the subject")
	assert.Equal(t, SystemActor(), e.Actor)
	assert.Equal(t, Visibility{Level: "roles", Roles: []string{"System Manager"}}, e.Visibility)
}

func TestEmitRollsBackWithCallerTransaction(t *testing.T) {
	pool := storetest.NewPool(t)
	v := newValidator(t)
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)
	_, err = Emit(t.Context(), tx, v, testNow, systemErrorInput("X", gen.SystemErrorData{Code: "TEST", Message: "x"}))
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(t.Context()))

	assert.Empty(t, loadRows(t, pool))
	again := emitCommitted(t, pool, v, testNow, systemErrorInput("X", gen.SystemErrorData{Code: "TEST", Message: "x"}))
	assert.Equal(t, int64(1), decodeStored(t, v, loadRow(t, pool, again).Payload).Seq, "rolled-back seq is reused")
}

func TestEmitPayloadIsNotHTMLEscaped(t *testing.T) {
	pool := storetest.NewPool(t)
	v := newValidator(t)
	id := emitCommitted(t, pool, v, testNow, systemErrorInput("X", gen.SystemErrorData{Code: "TEST", Message: "ทดสอบ & <ok>"}))
	payload := loadRow(t, pool, id).Payload
	assert.Contains(t, payload, `"message":"ทดสอบ & <ok>"`)
	var probe map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &probe))
}
