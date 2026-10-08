package erpnext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// workflowTTL is how long a doctype's workflow definition is reused before it
// is read again (it changes only when an administrator edits the workflow).
const workflowTTL = 5 * time.Minute

// workflowMeta is the part of an active Frappe Workflow the adapter needs.
type workflowMeta struct {
	stateField string
	docStatus  map[string]int // state -> docstatus it puts the document in
}

type cachedWorkflow struct {
	wf *workflowMeta // nil: the doctype has no active workflow
	at time.Time
}

// flexInt reads Frappe's doc_status, a Select stored as "0"/"1"/"2" (or a number).
type flexInt int

func (n *flexInt) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, `"`)
	if len(b) == 0 || string(b) == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.Atoi(string(b))
	if err != nil {
		return fmt.Errorf("erpnext: doc_status %q: %w", b, err)
	}
	*n = flexInt(v)
	return nil
}

// metaBundle is the part of frappe.desk.form.load.getdoctype the adapter reads:
// Desk's own source for a doctype's workflow, open to every user of the doctype
// (the Workflow doctype itself is readable by System Managers only).
type metaBundle struct {
	Docs []struct {
		Name         string `json:"name"`
		WorkflowDocs []struct {
			DocType    string `json:"doctype"`
			IsActive   int    `json:"is_active"`
			StateField string `json:"workflow_state_field"`
			States     []struct {
				State     string  `json:"state"`
				DocStatus flexInt `json:"doc_status"`
			} `json:"states"`
		} `json:"__workflow_docs"`
	} `json:"docs"`
}

// workflow returns dt's active workflow, nil when it has none.
func (a *Adapter) workflow(ctx context.Context, as erp.Principal, dt erp.DocType) (*workflowMeta, error) {
	a.workflowMu.Lock()
	c, ok := a.workflows[dt]
	a.workflowMu.Unlock()
	if ok && time.Since(c.at) < workflowTTL {
		return c.wf, nil
	}

	var bundle metaBundle
	q := url.Values{"doctype": {string(dt)}}
	if err := a.c.GetBody(ctx, as, "/api/method/frappe.desk.form.load.getdoctype", q, &bundle); err != nil {
		return nil, err
	}
	var wf *workflowMeta
	for _, d := range bundle.Docs {
		if d.Name != string(dt) {
			continue
		}
		for _, w := range d.WorkflowDocs {
			if w.DocType != "Workflow" || w.IsActive == 0 {
				continue
			}
			wf = &workflowMeta{stateField: w.StateField, docStatus: map[string]int{}}
			for _, s := range w.States {
				wf.docStatus[s.State] = int(s.DocStatus)
			}
		}
	}
	a.workflowMu.Lock()
	a.workflows[dt] = cachedWorkflow{wf: wf, at: time.Now()}
	a.workflowMu.Unlock()
	return wf, nil
}

type transition struct {
	Action    string `json:"action"`
	NextState string `json:"next_state"`
}

func (a *Adapter) transitions(ctx context.Context, as erp.Principal, doc rawDoc) ([]transition, error) {
	var out []transition
	if err := a.c.Call(ctx, as, "frappe.model.workflow.get_transitions", map[string]any{"doc": doc}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// applyTransition runs action on doc; Submitted when it took a draft to docstatus 1.
func (a *Adapter) applyTransition(ctx context.Context, as erp.Principal, wf *workflowMeta, doc rawDoc, action string) (erp.SubmitResult, error) {
	var out rawDoc
	args := map[string]any{"doc": doc, "action": action}
	if err := a.c.Call(ctx, as, "frappe.model.workflow.apply_workflow", args, &out); err != nil {
		return erp.SubmitResult{}, onWorkflow(err)
	}
	return erp.SubmitResult{
		Submitted:     doc.docstatus() == 0 && out.docstatus() == 1,
		WorkflowState: out.str(wf.stateField),
	}, nil
}

// onWorkflow files a rejected transition (WorkflowTransitionError) under "workflow".
func onWorkflow(err error) error {
	var ve *erp.ValidationError
	if errors.As(err, &ve) && len(ve.Fields) == 0 {
		return &erp.ValidationError{Message: ve.Message, Fields: map[string]string{"workflow": ve.Message}}
	}
	return err
}

func noWorkflow(dt erp.DocType) error {
	return erp.NewValidation("workflow", string(dt)+" has no workflow")
}

// Actions lists the workflow actions the principal may apply now ([] without a workflow).
func (a *Adapter) Actions(ctx context.Context, as erp.Principal, ref erp.DocRef) ([]string, error) {
	wf, err := a.workflow(ctx, as, ref.DocType)
	if err != nil {
		return nil, err
	}
	var doc rawDoc
	if err := a.getDoc(ctx, as, ref, &doc); err != nil {
		return nil, err
	}
	out := []string{}
	if wf == nil {
		return out, nil
	}
	ts, err := a.transitions(ctx, as, doc)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		out = appendUnique(out, t.Action)
	}
	return out, nil
}

// Apply runs one workflow action; ERPNext refuses actions the principal may not take.
func (a *Adapter) Apply(ctx context.Context, as erp.Principal, ref erp.DocRef, action string) (erp.SubmitResult, error) {
	wf, err := a.workflow(ctx, as, ref.DocType)
	if err != nil {
		return erp.SubmitResult{}, err
	}
	if wf == nil {
		return erp.SubmitResult{}, noWorkflow(ref.DocType)
	}
	var doc rawDoc
	if err := a.getDoc(ctx, as, ref, &doc); err != nil {
		return erp.SubmitResult{}, err
	}
	return a.applyTransition(ctx, as, wf, doc, action)
}

// pickTransition implements the Submit rule: the transition into a submitted
// state, else the only one available, else none.
func pickTransition(wf *workflowMeta, ts []transition) (transition, bool) {
	for _, t := range ts {
		if wf.docStatus[t.NextState] == 1 {
			return t, true
		}
	}
	if len(ts) == 1 {
		return ts[0], true
	}
	return transition{}, false
}
