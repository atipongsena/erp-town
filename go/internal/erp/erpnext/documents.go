package erpnext

import (
	"context"
	"fmt"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// Submit follows erp.Documents: without a workflow frappe.client.submit; with
// one, the transition into a submitted state, else the single available one
// (an approval request), else a ValidationError on "workflow".
func (a *Adapter) Submit(ctx context.Context, as erp.Principal, ref erp.DocRef) (erp.SubmitResult, error) {
	wf, err := a.workflow(ctx, as, ref.DocType)
	if err != nil {
		return erp.SubmitResult{}, err
	}
	var doc rawDoc
	if err := a.getDoc(ctx, as, ref, &doc); err != nil {
		return erp.SubmitResult{}, err
	}
	if ds := doc.docstatus(); ds != 0 {
		status, _ := docStatus(ds)
		return erp.SubmitResult{}, fmt.Errorf("%w: %s %s is %s", erp.ErrConflict, ref.DocType, ref.Name, status)
	}

	if wf == nil {
		var out rawDoc
		if err := a.c.Call(ctx, as, "frappe.client.submit", map[string]any{"doc": doc}, &out); err != nil {
			return erp.SubmitResult{}, err
		}
		if out.docstatus() != 1 {
			return erp.SubmitResult{}, fmt.Errorf("erpnext: %s %s not submitted (docstatus %d)", ref.DocType, ref.Name, out.docstatus())
		}
		return erp.SubmitResult{Submitted: true}, nil
	}

	ts, err := a.transitions(ctx, as, doc)
	if err != nil {
		return erp.SubmitResult{}, err
	}
	t, ok := pickTransition(wf, ts)
	if !ok {
		state := doc.str(wf.stateField)
		return erp.SubmitResult{}, &erp.ValidationError{
			Message: "no workflow transition available",
			Fields:  map[string]string{"workflow": fmt.Sprintf("no single transition to take from state %s (%d available)", state, len(ts))},
		}
	}
	return a.applyTransition(ctx, as, wf, doc, t.Action)
}

// Cancel cancels a submitted document (frappe.client.cancel).
func (a *Adapter) Cancel(ctx context.Context, as erp.Principal, ref erp.DocRef) error {
	args := map[string]any{"doctype": string(ref.DocType), "name": ref.Name}
	return a.c.Call(ctx, as, "frappe.client.cancel", args, nil)
}
