package world

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/events"
)

const maxSnapshotBytes = 32 * 1024 * 1024

var snapshotValidator = sync.OnceValues(events.NewValidator)

type snapshotSequence struct {
	Subject events.Subject `json:"subject"`
	Seq     int64          `json:"seq"`
}

type snapshotFacetFence struct {
	Key EntityKey `json:"key"`
	Seq int64     `json:"seq"`
}

// Payload bytes are base64 inside the internal JSON format so persistence keeps
// the original validated bytes, including whitespace, rather than compacting a
// json.RawMessage and changing an otherwise identical entity on restart.
type snapshotEntity struct {
	Entity
	Data []byte
}

type snapshotData struct {
	FormatVersion int                  `json:"format_version"`
	Company       string               `json:"company"`
	Version       uint64               `json:"version"`
	Entities      []snapshotEntity     `json:"entities"`
	Sequences     []snapshotSequence   `json:"seqs"`
	FacetFences   []snapshotFacetFence `json:"facet_fences"`
}

// EncodeSnapshot persists internal state, including sequence tombstones. It is
// versioned independently from the filtered client protocol and deterministic.
func EncodeSnapshot(s State) ([]byte, error) {
	data := snapshotData{FormatVersion: 2, Company: s.company, Version: s.version, Entities: make([]snapshotEntity, 0, len(s.entities)), Sequences: make([]snapshotSequence, 0, len(s.seqs)), FacetFences: make([]snapshotFacetFence, 0, len(s.reconciled))}
	for _, entity := range s.Entities() {
		data.Entities = append(data.Entities, snapshotEntity{Entity: entity, Data: bytes.Clone(entity.Data)})
	}
	for subject, seq := range s.seqs {
		data.Sequences = append(data.Sequences, snapshotSequence{Subject: subject, Seq: seq})
	}
	for key, seq := range s.reconciled {
		data.FacetFences = append(data.FacetFences, snapshotFacetFence{Key: key, Seq: seq})
	}
	slices.SortFunc(data.FacetFences, func(a, b snapshotFacetFence) int {
		return cmp.Or(cmp.Compare(a.Key.Subject.DocType, b.Key.Subject.DocType), cmp.Compare(a.Key.Subject.Name, b.Key.Subject.Name), cmp.Compare(a.Key.Kind, b.Key.Kind))
	})
	slices.SortFunc(data.Sequences, func(a, b snapshotSequence) int {
		return cmp.Or(cmp.Compare(a.Subject.DocType, b.Subject.DocType), cmp.Compare(a.Subject.Name, b.Subject.Name))
	})
	if _, err := stateFromSnapshot(data); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("world: encode snapshot: %w", err)
	}
	if len(raw) > maxSnapshotBytes {
		return nil, errors.New("world: snapshot exceeds 32 MiB")
	}
	return raw, nil
}

// DecodeSnapshot rejects corrupt or unsupported formats instead of accepting a
// partially restored projection. Returned state owns its maps and payload data.
func DecodeSnapshot(raw []byte) (State, error) {
	if len(raw) > maxSnapshotBytes {
		return State{}, errors.New("world: snapshot exceeds 32 MiB")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return State{}, fmt.Errorf("world: decode snapshot: %w", err)
	}
	var data snapshotData
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return State{}, fmt.Errorf("world: decode snapshot fields: %w", err)
	}
	for _, key := range []string{"format_version", "company", "version", "entities", "seqs", "facet_fences"} {
		field, exists := fields[key]
		if !exists || bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
			return State{}, fmt.Errorf("world: missing snapshot field %s", key)
		}
	}
	if err := strictSnapshotJSON(raw, &data); err != nil {
		return State{}, fmt.Errorf("world: decode snapshot: %w", err)
	}
	return stateFromSnapshot(data)
}

func stateFromSnapshot(data snapshotData) (State, error) {
	if data.FormatVersion != 2 {
		return State{}, fmt.Errorf("world: unsupported snapshot format %d", data.FormatVersion)
	}
	s, err := NewState(data.Company)
	if err != nil {
		return State{}, err
	}
	s.version = data.Version
	for _, fence := range data.FacetFences {
		if !validFacetFence(fence.Key) || fence.Seq <= 0 {
			return State{}, errors.New("world: invalid reconciliation fence")
		}
		if _, exists := s.reconciled[fence.Key]; exists {
			return State{}, errors.New("world: duplicate reconciliation fence")
		}
		s.reconciled[fence.Key] = fence.Seq
	}
	for _, entry := range data.Sequences {
		if !validSnapshotSubject(entry.Subject) || entry.Seq < 0 {
			return State{}, errors.New("world: invalid snapshot sequence")
		}
		if _, exists := s.seqs[entry.Subject]; exists {
			return State{}, errors.New("world: duplicate snapshot sequence")
		}
		s.seqs[entry.Subject] = entry.Seq
	}
	for _, stored := range data.Entities {
		entity := stored.Entity
		entity.Data = bytes.Clone(stored.Data)
		seq, exists := s.seqs[entity.Key.Subject]
		if !exists || entity.Seq < 0 || entity.Seq > seq || entity.Version == 0 || entity.Version > s.version {
			return State{}, errors.New("world: inconsistent snapshot entity version or sequence")
		}
		if _, exists := s.entities[entity.Key]; exists {
			return State{}, errors.New("world: duplicate snapshot entity")
		}
		if err := validateSnapshotEntity(s.company, entity); err != nil {
			return State{}, err
		}
		s.entities[entity.Key] = cloneEntity(entity)
	}
	if s.version == 0 && (len(s.seqs) > 0 || len(s.reconciled) > 0) {
		return State{}, errors.New("world: unversioned snapshot contains sequence fences")
	}
	return s, nil
}

func validSnapshotSubject(subject events.Subject) bool {
	return strings.TrimSpace(subject.DocType) != "" && strings.TrimSpace(subject.Name) != ""
}

func validateSnapshotEntity(company string, entity Entity) error {
	if err := rejectDuplicateJSONKeys(entity.Data); err != nil {
		return fmt.Errorf("world: invalid snapshot payload JSON: %w", err)
	}
	if strings.HasPrefix(entity.EventType, "world.projection.") {
		return validateProjectionSnapshotEntity(entity)
	}
	v, err := snapshotValidator()
	if err != nil {
		return err
	}
	event := events.Envelope{
		SpecVersion: "1.0", SchemaVersion: 1, Source: "town/snapshot", Company: &company,
		ID: entity.EventID, Type: entity.EventType, Time: entity.Time, Seq: entity.Seq,
		Subject: entity.Key.Subject, Actor: entity.Actor, CorrelationID: entity.CorrelationID,
		Visibility: entity.Visibility, Data: entity.Data,
	}
	// Agent envelopes have a fixed Go-owned producer. Older ERP snapshots keep
	// their existing reconstruction source and format; no sequence is rewritten.
	if strings.HasPrefix(entity.EventType, "agent.") {
		event.Source = "town/agents"
	}
	if strings.HasPrefix(entity.EventType, "dev.") {
		event.Source = "town/devbridge"
	}
	raw, err := events.Marshal(event)
	if err != nil {
		return err
	}
	if err := v.Validate(raw); err != nil {
		return fmt.Errorf("world: invalid snapshot event: %w", err)
	}
	key, err := entityKey(event)
	if err != nil {
		return err
	}
	if key != entity.Key || entity.EventType == "workflow.approval.decided" {
		return errors.New("world: snapshot entity does not match event key")
	}
	return nil
}

func validateProjectionSnapshotEntity(entity Entity) error {
	if !validSnapshotSubject(entity.Key.Subject) || entity.EventID != "" || entity.CorrelationID != "" ||
		entity.Actor.Kind != "system" || entity.Actor.ID != "system.town" || entity.Actor.OnBehalfOf != nil ||
		entity.Visibility.Level != "public" || len(entity.Visibility.Roles) != 0 {
		return errors.New("world: invalid reconciled snapshot metadata")
	}
	if _, err := time.Parse(time.RFC3339Nano, entity.Time); err != nil {
		return fmt.Errorf("world: invalid reconciled snapshot time: %w", err)
	}
	valid := false
	switch entity.EventType {
	case "world.projection.document":
		var data struct {
			Status *string `json:"status"`
		}
		if err := strictSnapshotJSON(entity.Data, &data); err != nil {
			return err
		}
		valid = entity.Key.Kind == KindDocument && entity.Key.Scope == "" &&
			(entity.Key.Subject.DocType == "Sales Order" || entity.Key.Subject.DocType == "Delivery Note") && data.Status != nil && strings.TrimSpace(*data.Status) != ""
	case "world.projection.overdue":
		var data struct {
			DueDate     *string `json:"due_date"`
			DaysOverdue *int64  `json:"days_overdue"`
		}
		if err := strictSnapshotJSON(entity.Data, &data); err != nil {
			return err
		}
		valid = entity.Key.Kind == KindOverdue && entity.Key.Scope == "" && entity.Key.Subject.DocType == "Sales Invoice" && data.DueDate != nil && data.DaysOverdue != nil && *data.DaysOverdue >= 1
		if valid {
			_, err := erp.ParseDate(*data.DueDate)
			valid = err == nil
		}
	case "world.projection.stock":
		var data struct {
			Warehouse *string  `json:"warehouse"`
			FillRatio *float64 `json:"fill_ratio"`
		}
		if err := strictSnapshotJSON(entity.Data, &data); err != nil {
			return err
		}
		valid = entity.Key.Kind == KindStock && entity.Key.Subject.DocType == "Item" && data.Warehouse != nil && strings.TrimSpace(*data.Warehouse) != "" && *data.Warehouse == entity.Key.Scope && data.FillRatio != nil && *data.FillRatio >= 0 && *data.FillRatio <= 1
	}
	if !valid {
		return errors.New("world: invalid reconciled snapshot fact")
	}
	return nil
}

func strictSnapshotJSON(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("world: trailing snapshot JSON")
	}
	return nil
}

// encoding/json accepts duplicate object keys; persisted state must not depend
// on which parser wins. Check them throughout nested metadata and raw payloads.
func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("snapshot nesting exceeds 64")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, isDelim := token.(json.Delim)
		if !isDelim {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate or invalid snapshot JSON key")
				}
				seen[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid snapshot JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing snapshot JSON")
	}
	return nil
}
