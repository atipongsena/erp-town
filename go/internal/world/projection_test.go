package world

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/events"
)

func fixture(t *testing.T, name string) events.Envelope {
	t.Helper()
	raw, err := os.ReadFile("../events/schema/fixtures/valid/" + name + ".json")
	require.NoError(t, err)
	v, err := events.NewValidator()
	require.NoError(t, err)
	e, err := events.Decode(v, raw)
	require.NoError(t, err)
	return e
}

func setup(t *testing.T) (*Projector, State) {
	t.Helper()
	p, err := NewProjector()
	require.NoError(t, err)
	s, err := NewState("ACME-TH")
	require.NoError(t, err)
	return p, s
}

func apply(t *testing.T, p *Projector, s State, e events.Envelope) State {
	t.Helper()
	next, err := p.Apply(s, e)
	require.NoError(t, err)
	return next
}

func TestProjectionCoversCurrentEventContract(t *testing.T) {
	for _, tc := range []struct {
		event, kind string
		count       int
	}{
		{"erp.sales_order.submitted", "document", 1},
		{"erp.sales_order.cancelled", "document", 1},
		{"erp.delivery_note.submitted", "document", 1},
		{"erp.delivery_note.cancelled", "document", 1},
		{"erp.sales_invoice.submitted", "document", 1},
		{"erp.sales_invoice.cancelled", "document", 1},
		{"erp.payment_entry.submitted", "document", 1},
		{"erp.payment_entry.cancelled", "document", 1},
		{"erp.sales_invoice.overdue", "overdue", 1},
		{"erp.stock.below_reorder", "stock", 1},
		{"workflow.approval.requested", "approval", 1},
		{"workflow.approval.decided", "", 0},
		{"system.error", "error", 1},
	} {
		t.Run(tc.event, func(t *testing.T) {
			p, s := setup(t)
			e := fixture(t, tc.event)
			next := apply(t, p, s, e)
			entities := next.Entities()
			require.Len(t, entities, tc.count)
			if tc.count > 0 {
				assert.Equal(t, tc.kind, string(entities[0].Key.Kind))
				assert.Equal(t, e.Subject, entities[0].Key.Subject)
				assert.Equal(t, e.Type, entities[0].EventType)
				assert.JSONEq(t, string(e.Data), string(entities[0].Data))
			}
			assert.Empty(t, s.Entities(), "Apply must preserve the input state")
		})
	}
}

func TestProjectionIgnoresOtherCompanies(t *testing.T) {
	p, s := setup(t)
	e := fixture(t, "erp.sales_order.submitted")
	other := "OTHER"
	e.Company = &other
	next := apply(t, p, s, e)
	assert.Equal(t, s, next)
	assert.Equal(t, int64(0), next.LastSeq(e.Subject))
	e.Company = nil
	assert.Equal(t, s, apply(t, p, s, e))
}

func TestProjectionRejectsEmptyCompany(t *testing.T) {
	for _, company := range []string{"", " \t"} {
		_, err := NewState(company)
		require.Error(t, err)
	}
}

func TestProjectionDuplicateAndStaleEventsDoNotChangeState(t *testing.T) {
	p, s := setup(t)
	e := fixture(t, "erp.sales_order.submitted")
	e.Seq = 3
	next := apply(t, p, s, e)
	assert.Equal(t, next, apply(t, p, next, e))
	stale := fixture(t, "erp.sales_order.cancelled")
	stale.Subject = e.Subject
	stale.Seq = 2
	assert.Equal(t, next, apply(t, p, next, stale))
	assert.Equal(t, int64(3), next.LastSeq(e.Subject))
	assert.Equal(t, uint64(1), next.Version())
	assert.Equal(t, "ACME-TH", next.Company())
}

func TestProjectionAcceptsSeqGapsAndIndependentSubjects(t *testing.T) {
	p, s := setup(t)
	e := fixture(t, "erp.sales_order.submitted")
	s = apply(t, p, s, e)
	cancel := fixture(t, "erp.sales_order.cancelled")
	cancel.Subject = e.Subject
	cancel.Seq = 10
	s = apply(t, p, s, cancel)
	other := fixture(t, "erp.sales_order.submitted")
	other.Subject.Name = "SO-OTHER"
	s = apply(t, p, s, other)
	assert.Equal(t, int64(10), s.LastSeq(e.Subject))
	assert.Equal(t, int64(1), s.LastSeq(other.Subject))
	assert.Len(t, s.Entities(), 2)
	assert.Equal(t, uint64(3), s.Version())
}

func TestProjectionApprovalDecisionRemovesPendingSignal(t *testing.T) {
	for _, decision := range []string{"approved", "rejected"} {
		t.Run(decision, func(t *testing.T) {
			p, s := setup(t)
			requested := fixture(t, "workflow.approval.requested")
			s = apply(t, p, s, requested)
			require.Len(t, s.Entities(), 1)
			decided := fixture(t, "workflow.approval.decided")
			decided.Subject = requested.Subject
			decided.Seq = 2
			var data map[string]any
			require.NoError(t, json.Unmarshal(decided.Data, &data))
			data["decision"] = decision
			decided.Data, _ = json.Marshal(data)
			next := apply(t, p, s, decided)
			assert.Empty(t, next.Entities())
			assert.Len(t, s.Entities(), 1)
			assert.Equal(t, next, apply(t, p, next, requested), "old requests must not resurrect the signal")
		})
	}
}

func TestProjectionCancellationRemovesSubjectSignals(t *testing.T) {
	for _, tc := range []struct{ signal, cancel string }{
		{"erp.sales_invoice.overdue", "erp.sales_invoice.cancelled"},
		{"workflow.approval.requested", "erp.sales_order.cancelled"},
	} {
		t.Run(tc.signal, func(t *testing.T) {
			p, s := setup(t)
			signal := fixture(t, tc.signal)
			s = apply(t, p, s, signal)
			cancel := fixture(t, tc.cancel)
			cancel.Subject = signal.Subject
			cancel.Seq = signal.Seq + 1
			next := apply(t, p, s, cancel)
			entities := next.Entities()
			require.Len(t, entities, 1)
			assert.Equal(t, "document", string(entities[0].Key.Kind))
			assert.Equal(t, cancel.Type, entities[0].EventType)
			assert.JSONEq(t, `{"status":"Cancelled"}`, string(entities[0].Data))
		})
	}
}

func TestProjectionPaymentDoesNotInventASettledInvoice(t *testing.T) {
	p, s := setup(t)
	overdue := fixture(t, "erp.sales_invoice.overdue")
	s = apply(t, p, s, overdue)
	paid := fixture(t, "erp.payment_entry.submitted")
	paid.Data = json.RawMessage(`{"party":"CUST-001","mode_of_payment":null,"against":["` + overdue.Subject.Name + `"]}`)
	s = apply(t, p, s, paid)
	assert.Len(t, s.Entities(), 2, "payment events contain no balance and may be partial")
}

func TestProjectionKeepsStockSignalsPerWarehouse(t *testing.T) {
	p, s := setup(t)
	e := fixture(t, "erp.stock.below_reorder")
	s = apply(t, p, s, e)
	e.ID = "01JA0K6Y8Z9P3R4S5T6V7W8XB9"
	e.Seq = 2
	e.Data = json.RawMessage(`{"warehouse":"Annex - ACME","fill_ratio":0.5}`)
	s = apply(t, p, s, e)
	entities := s.Entities()
	require.Len(t, entities, 2)
	assert.Equal(t, "Annex - ACME", entities[0].Key.Scope)
	assert.Equal(t, "Stores - ACME", entities[1].Key.Scope)
}

func TestProjectionRetainsVisibilityPerSignalWithoutMergingPayloads(t *testing.T) {
	p, s := setup(t)
	doc := fixture(t, "erp.sales_invoice.submitted")
	doc.Visibility = events.Visibility{Level: "roles", Roles: []string{"Accounts User"}}
	s = apply(t, p, s, doc)
	overdue := fixture(t, "erp.sales_invoice.overdue")
	overdue.Subject = doc.Subject
	overdue.Seq = doc.Seq + 1
	s = apply(t, p, s, overdue)
	entities := s.Entities()
	require.Len(t, entities, 2)
	assert.Equal(t, "roles", entities[0].Visibility.Level)
	assert.Equal(t, []string{"Accounts User"}, entities[0].Visibility.Roles)
	assert.Equal(t, "public", entities[1].Visibility.Level)
	assert.NotContains(t, string(entities[1].Data), "party")
	assert.NotContains(t, string(entities[1].Data), "against")
}

func TestProjectionStateDoesNotAliasInputsOrSnapshots(t *testing.T) {
	p, s := setup(t)
	e := fixture(t, "workflow.approval.requested")
	e.Visibility = events.Visibility{Level: "roles", Roles: []string{"Sales Manager"}}
	s = apply(t, p, s, e)
	e.Data[0] = '!'
	e.Visibility.Roles[0] = "Other"
	*e.Actor.OnBehalfOf = "Other"
	snapshot := s.Entities()
	require.Len(t, snapshot, 1)
	assert.True(t, json.Valid(snapshot[0].Data))
	assert.Equal(t, []string{"Sales Manager"}, snapshot[0].Visibility.Roles)
	assert.Equal(t, "user.somchai@acme.example", *snapshot[0].Actor.OnBehalfOf)
	snapshot[0].Data[0] = '!'
	snapshot[0].Visibility.Roles[0] = "Other"
	*snapshot[0].Actor.OnBehalfOf = "Other"
	again := s.Entities()
	assert.True(t, json.Valid(again[0].Data))
	assert.Equal(t, []string{"Sales Manager"}, again[0].Visibility.Roles)
	assert.Equal(t, "user.somchai@acme.example", *again[0].Actor.OnBehalfOf)
}

func TestProjectionInvalidEventLeavesStateAndSeqUnchanged(t *testing.T) {
	p, s := setup(t)
	e := fixture(t, "erp.sales_order.submitted")
	s = apply(t, p, s, e)
	e.Seq++
	e.Data = json.RawMessage(`{"grand_total":100}`)
	next, err := p.Apply(s, e)
	require.Error(t, err)
	assert.Equal(t, s, next)
	assert.Equal(t, int64(1), next.LastSeq(e.Subject))
}

func TestProjectionCompanyErrorDoesNotOverwriteDocument(t *testing.T) {
	p, s := setup(t)
	doc := fixture(t, "erp.sales_order.submitted")
	s = apply(t, p, s, doc)
	e := fixture(t, "system.error")
	e.Company = doc.Company
	e.Subject = doc.Subject
	e.Seq = doc.Seq + 1
	s = apply(t, p, s, e)
	entities := s.Entities()
	require.Len(t, entities, 2)
	assert.Equal(t, "document", string(entities[0].Key.Kind))
	assert.Equal(t, doc.Type, entities[0].EventType)
	assert.Equal(t, "error", string(entities[1].Key.Kind))
}
