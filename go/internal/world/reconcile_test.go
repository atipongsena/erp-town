package world

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/events"
)

func TestReconcileClearsSignalsAndKeepsReplayFences(t *testing.T) {
	p, state := setup(t)
	e := fixture(t, "erp.sales_invoice.overdue")
	state = apply(t, p, state, e)
	ref := erp.DocRef{DocType: erp.DocSalesInvoice, Name: e.Subject.Name}
	current := erp.ProjectionSnapshot{Company: state.Company(), Facts: []erp.ProjectionFact{}, Sequences: []erp.ProjectionSequence{{Ref: ref, Seq: e.Seq + 2}}}
	updated, err := Reconcile(state, current, time.Now())
	require.NoError(t, err)
	assert.Empty(t, updated.Entities())
	assert.Equal(t, e.Seq, updated.LastSeq(e.Subject), "ERP fences must not pretend other event facets were applied")
	e.Seq++
	assert.Equal(t, updated, apply(t, p, updated, e), "late overdue cannot restore a paid signal")
	raw, err := EncodeSnapshot(updated)
	require.NoError(t, err)
	restored, err := DecodeSnapshot(raw)
	require.NoError(t, err)
	assert.Equal(t, restored, apply(t, p, restored, e), "reconciliation fence survives restart")
}

func TestReconcileDoesNotSkipApprovalFacet(t *testing.T) {
	p, state := setup(t)
	doc := fixture(t, "erp.sales_order.submitted")
	doc.Seq = 1
	state = apply(t, p, state, doc)
	ref := erp.DocRef{DocType: erp.DocSalesOrder, Name: doc.Subject.Name}
	current := erp.ProjectionSnapshot{Company: state.Company(), Facts: []erp.ProjectionFact{{Ref: ref, Kind: "document", Status: "To Deliver"}}, Sequences: []erp.ProjectionSequence{{Ref: ref, Seq: 3}}}
	updated, err := Reconcile(state, current, time.Now())
	require.NoError(t, err)
	approval := fixture(t, "workflow.approval.requested")
	approval.Subject = doc.Subject
	approval.Seq = 2
	updated = apply(t, p, updated, approval)
	require.Len(t, updated.Entities(), 2)
	assert.Equal(t, int64(2), updated.LastSeq(doc.Subject))
	assert.NotEqual(t, string(updated.Entities()[0].Data), string(updated.Entities()[1].Data), "metadata and approval remain independent")
	unchanged, err := Reconcile(updated, current, time.Now().Add(time.Minute))
	require.NoError(t, err)
	assert.Equal(t, updated, unchanged, "same ERP metadata must not generate a new version just for the clock")
}

func TestReconcileStockFenceCoversNewWarehouses(t *testing.T) {
	p, state := setup(t)
	e := fixture(t, "erp.stock.below_reorder")
	state = apply(t, p, state, e)
	current := erp.ProjectionSnapshot{Company: state.Company(), Facts: []erp.ProjectionFact{}, Sequences: []erp.ProjectionSequence{{Ref: erp.DocRef{DocType: "Item", Name: e.Subject.Name}, Seq: e.Seq + 2}}}
	updated, err := Reconcile(state, current, time.Now())
	require.NoError(t, err)
	e.Seq++
	e.Data = []byte(`{"warehouse":"ANOTHER-WH","fill_ratio":0.5}`)
	assert.Equal(t, updated, apply(t, p, updated, e))
	e.Seq += 2
	assert.Len(t, apply(t, p, updated, e).Entities(), 1, "a genuinely newer stock crossing is accepted")
}

func TestReconcileRejectsPartialReadAndPreservesEarlierState(t *testing.T) {
	_, state := setup(t)
	_, err := Reconcile(state, erp.ProjectionSnapshot{Company: state.Company()}, time.Now())
	require.Error(t, err)
	_, err = Reconcile(state, erp.ProjectionSnapshot{Company: state.Company(), Facts: []erp.ProjectionFact{}, Sequences: []erp.ProjectionSequence{}}, time.Time{})
	require.Error(t, err)
}

func TestReconcilePreservesAudienceAndRejectsStaleRead(t *testing.T) {
	for _, visibility := range []events.Visibility{{Level: "private"}, {Level: "roles", Roles: []string{"Sales User"}}} {
		p, state := setup(t)
		e := fixture(t, "erp.sales_order.submitted")
		e.Visibility = visibility
		state = apply(t, p, state, e)
		current := erp.ProjectionSnapshot{Company: state.Company(), Facts: []erp.ProjectionFact{{Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: e.Subject.Name}, Kind: "document", Status: "To Deliver"}}, Sequences: []erp.ProjectionSequence{{Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: e.Subject.Name}, Seq: e.Seq + 2}}}
		updated, err := Reconcile(state, current, time.Now())
		require.NoError(t, err)
		assert.Equal(t, state.Entities(), updated.Entities(), "ERP metadata cannot promote or replace a nonpublic event payload")
		current.Sequences[0].Seq--
		rejected, err := Reconcile(updated, current, time.Now())
		require.ErrorContains(t, err, "stale ERP projection")
		assert.Equal(t, updated, rejected)
		current.Sequences[0].Seq = e.Seq - 1
		rejected, err = Reconcile(state, current, time.Now())
		require.ErrorContains(t, err, "stale ERP projection")
		assert.Equal(t, state, rejected)
	}
}
