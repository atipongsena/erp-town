package world

import (
	"errors"
	"fmt"
	"strings"

	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/atipongsena/erp-town/go/internal/events/gen"
)

// Projector is a pure reducer using the existing event contract. Its validator is
// compiled once and immutable; Apply performs no I/O and reads no clock.
type Projector struct {
	validator *events.Validator
}

func NewProjector() (*Projector, error) {
	v, err := events.NewValidator()
	if err != nil {
		return nil, err
	}
	return &Projector{validator: v}, nil
}

// Apply returns a new state for a valid, newer event belonging to its company.
// Invalid input returns the original state and an error; another company, a
// company-null event or an already-applied seq returns the original state unchanged.
// Lifecycle events record document activity, not a claim about the current ERP
// backlog. Payments cannot prove an invoice is settled; stock alerts need future
// read-only reconciliation to learn when stock has recovered.
func (p *Projector) Apply(s State, e events.Envelope) (State, error) {
	if s.company == "" {
		return s, errors.New("world: state must be created with NewState")
	}
	raw, err := events.Marshal(e)
	if err != nil {
		return s, fmt.Errorf("world: encode event: %w", err)
	}
	if err := p.validator.Validate(raw); err != nil {
		return s, fmt.Errorf("world: invalid event: %w", err)
	}
	if e.Company == nil || *e.Company != s.company || e.Seq <= s.LastSeq(e.Subject) {
		return s, nil
	}
	key, err := entityKey(e)
	if err != nil {
		return s, err
	}
	if e.Seq <= s.reconciled[EntityKey{Subject: key.Subject, Kind: key.Kind}] {
		return s, nil
	}
	next := s.copyForWrite()
	next.version++
	next.seqs[e.Subject] = e.Seq
	if e.Type == "workflow.approval.decided" {
		delete(next.entities, key)
		return next, nil
	}
	if key.Kind == KindDocument && strings.HasSuffix(e.Type, ".cancelled") {
		delete(next.entities, EntityKey{Subject: e.Subject, Kind: KindApproval})
		delete(next.entities, EntityKey{Subject: e.Subject, Kind: KindOverdue})
	}
	next.entities[key] = cloneEntity(Entity{
		Key: key, Version: next.version, EventID: e.ID, EventType: e.Type,
		Time: e.Time, Seq: e.Seq, CorrelationID: e.CorrelationID,
		Actor: e.Actor, Visibility: e.Visibility, Data: e.Data,
	})
	return next, nil
}

func entityKey(e events.Envelope) (EntityKey, error) {
	key := EntityKey{Subject: e.Subject}
	switch e.Type {
	case "erp.sales_order.submitted", "erp.sales_order.cancelled",
		"erp.delivery_note.submitted", "erp.delivery_note.cancelled",
		"erp.sales_invoice.submitted", "erp.sales_invoice.cancelled",
		"erp.payment_entry.submitted", "erp.payment_entry.cancelled":
		key.Kind = KindDocument
	case "workflow.approval.requested", "workflow.approval.decided":
		key.Kind = KindApproval
	case "erp.sales_invoice.overdue":
		key.Kind = KindOverdue
	case "erp.stock.below_reorder":
		data, err := events.DecodeData[gen.ErpStockBelowReorderData](e)
		if err != nil {
			return EntityKey{}, err
		}
		key.Kind, key.Scope = KindStock, data.Warehouse
	case "system.error":
		key.Kind = KindError
	default:
		return EntityKey{}, fmt.Errorf("world: unsupported event type %q", e.Type)
	}
	return key, nil
}
