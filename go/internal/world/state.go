// Package world holds the town's event-derived projection. It has no ERP write
// capability. This package's state is an internal model, not the WebSocket format.
package world

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/atipongsena/erp-town/go/internal/events"
)

// EntityKind separates facts whose payload and visibility must remain independent.
type EntityKind string

const (
	KindDocument EntityKind = "document"
	KindApproval EntityKind = "approval"
	KindOverdue  EntityKind = "overdue"
	KindStock    EntityKind = "stock"
	KindError    EntityKind = "error"
)

// EntityKey identifies a document activity or signal. Scope is the warehouse for
// stock signals, since one Item can cross its reorder level in several warehouses.
type EntityKey struct {
	Subject events.Subject
	Kind    EntityKind
	Scope   string
}

// Entity is the latest event fact for a key, with its original visibility intact.
// Data contains only the validated event payload, never a fetched ERP document.
// Approval and overdue facts do not inherit the document fact's fields or audience.
type Entity struct {
	Key           EntityKey
	Version       uint64
	EventID       string
	EventType     string
	Time          string
	Seq           int64
	CorrelationID string
	Actor         events.Actor
	Visibility    events.Visibility
	Data          json.RawMessage
}

// State is an immutable projection for one company. Copies may share internal
// storage; Projector.Apply copies the maps before writing and Entities returns
// owned data. A caller cannot mutate an earlier state through an event or snapshot.
type State struct {
	company  string
	version  uint64
	entities map[EntityKey]Entity
	seqs     map[events.Subject]int64
	// ERP read fences apply only to the rebuilt facet. They must never consume
	// sequence numbers belonging to approval/error events we have not received.
	reconciled map[EntityKey]int64
}

func NewState(company string) (State, error) {
	if strings.TrimSpace(company) == "" {
		return State{}, errors.New("world: company code is required")
	}
	return State{company: company, entities: map[EntityKey]Entity{}, seqs: map[events.Subject]int64{}, reconciled: map[EntityKey]int64{}}, nil
}

func (s State) Company() string { return s.company }
func (s State) Version() uint64 { return s.version }

// LastSeq includes removed signals, preventing stale events from resurrecting them.
// Event-id deduplication belongs to the stream consumer; seq also makes replaying
// the same immutable envelope a no-op here. Gaps never prevent the next event.
func (s State) LastSeq(subject events.Subject) int64 { return s.seqs[subject] }

// Entities returns independent copies in a deterministic order. This is an internal
// snapshot; a future authenticated sync layer must filter it before sending it.
func (s State) Entities() []Entity {
	entities := make([]Entity, 0, len(s.entities))
	for _, e := range s.entities {
		entities = append(entities, cloneEntity(e))
	}
	slices.SortFunc(entities, func(a, b Entity) int {
		return cmp.Or(
			cmp.Compare(a.Key.Subject.DocType, b.Key.Subject.DocType),
			cmp.Compare(a.Key.Subject.Name, b.Key.Subject.Name),
			cmp.Compare(a.Key.Kind, b.Key.Kind),
			cmp.Compare(a.Key.Scope, b.Key.Scope),
		)
	})
	return entities
}

func (s State) copyForWrite() State {
	s.entities = maps.Clone(s.entities)
	s.seqs = maps.Clone(s.seqs)
	s.reconciled = maps.Clone(s.reconciled)
	return s
}

func cloneEntity(e Entity) Entity {
	e.Data = bytes.Clone(e.Data)
	e.Visibility.Roles = slices.Clone(e.Visibility.Roles)
	if e.Actor.OnBehalfOf != nil {
		id := *e.Actor.OnBehalfOf
		e.Actor.OnBehalfOf = &id
	}
	return e
}
