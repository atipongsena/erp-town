package world

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/events"
)

func validFacetFence(key EntityKey) bool {
	if !validSnapshotSubject(key.Subject) || key.Scope != "" {
		return false
	}
	switch key.Kind {
	case KindDocument:
		return key.Subject.DocType == "Sales Order" || key.Subject.DocType == "Delivery Note"
	case KindOverdue:
		return key.Subject.DocType == "Sales Invoice"
	case KindStock:
		return key.Subject.DocType == "Item"
	default:
		return false
	}
}

func fenceKey(ref erp.DocRef) EntityKey {
	key := EntityKey{Subject: events.Subject{DocType: string(ref.DocType), Name: ref.Name}}
	switch ref.DocType {
	case erp.DocSalesOrder, erp.DocDeliveryNote:
		key.Kind = KindDocument
	case erp.DocSalesInvoice:
		key.Kind = KindOverdue
	case "Item":
		key.Kind = KindStock
	}
	return key
}

// Reconcile repairs current backlog/signals using whitelisted ERP metadata.
// It fences only the affected facet, not the event sequence of the whole subject.
// The read must be complete and at least as new as already observed ERP events.
func Reconcile(state State, current erp.ProjectionSnapshot, at time.Time) (State, error) {
	if err := current.Validate(state.Company()); err != nil {
		return state, err
	}
	if at.IsZero() {
		return state, errors.New("world: reconciliation time is required")
	}
	next := state.copyForWrite()
	version := state.Version() + 1
	changed := false
	for _, seq := range current.Sequences {
		key := fenceKey(seq.Ref)
		if seq.Seq < state.LastSeq(key.Subject) || seq.Seq < state.reconciled[key] {
			return state, fmt.Errorf("world: stale ERP projection for %s/%s", seq.Ref.DocType, seq.Ref.Name)
		}
		if seq.Seq > next.reconciled[key] {
			next.reconciled[key] = seq.Seq
			changed = true
		}
	}
	facts := map[EntityKey]Entity{}
	for _, fact := range current.Facts {
		key := EntityKey{Subject: events.Subject{DocType: string(fact.Ref.DocType), Name: fact.Ref.Name}, Kind: EntityKind(fact.Kind), Scope: fact.Scope}
		var data any
		switch key.Kind {
		case KindDocument:
			data = struct {
				Status string `json:"status"`
			}{fact.Status}
		case KindOverdue:
			data = struct {
				DueDate     string `json:"due_date"`
				DaysOverdue int    `json:"days_overdue"`
			}{fact.DueDate.String(), fact.DaysOverdue}
		case KindStock:
			data = struct {
				Warehouse string      `json:"warehouse"`
				FillRatio json.Number `json:"fill_ratio"`
			}{fact.Scope, json.Number(fact.FillRatio.String())}
		}
		raw, err := json.Marshal(data)
		if err != nil {
			return state, err
		}
		facts[key] = Entity{Key: key, Version: version, EventType: "world.projection." + fact.Kind, Time: at.UTC().Format(time.RFC3339Nano), Seq: state.LastSeq(key.Subject), Actor: events.Actor{Kind: "system", ID: "system.town"}, Visibility: events.Visibility{Level: "public", Roles: []string{}}, Data: raw}
	}
	for key := range next.entities {
		managed := validFacetFence(EntityKey{Subject: key.Subject, Kind: key.Kind})
		if _, exists := facts[key]; managed && !exists {
			delete(next.entities, key)
			changed = true
		}
	}
	for key, entity := range facts {
		old, exists := next.entities[key]
		// Preserve the independent audience of a live event. Public metadata must
		// not turn an existing private/role fact into a publicly visible event.
		if exists && old.Visibility.Level != "public" {
			continue
		}
		if exists && old.EventType == entity.EventType && bytes.Equal(old.Data, entity.Data) {
			continue
		}
		next.entities[key] = entity
		if _, exists := next.seqs[key.Subject]; !exists {
			next.seqs[key.Subject] = 0
		}
		changed = true
	}
	if !changed {
		return state, nil
	}
	next.version = version
	return next, nil
}
