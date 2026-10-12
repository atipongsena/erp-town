package erpnext

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

const (
	companyName = "บริษัท ตัวอย่างเทรดดิ้ง จำกัด"
	soName      = "SO6910-0001"
	pathSO      = "/api/resource/Sales Order/" + soName
	pathMethod  = "/api/method/"
)

var sales = erp.Principal{
	Kind: erp.PrincipalUser, ID: "sales@acme-th.test",
	Credential: erp.SessionCredential{SessionID: "sess-sales", CSRFToken: "csrf-sales"},
}

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

// patchDoc rewrites fields of the document inside a {"data"|"message": {...}} fixture
// and returns it wrapped in envelope (numbers stay exact).
func patchDoc(t testing.TB, raw []byte, envelope string, set map[string]any) []byte {
	t.Helper()
	var env map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &env))
	body := env["data"]
	if body == nil {
		body = env["message"]
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc))
	for k, v := range set {
		doc[k] = v
	}
	out, err := json.Marshal(map[string]any{envelope: doc})
	require.NoError(t, err)
	return out
}

type call struct {
	Ctx          context.Context
	Method, Path string
	Query        string
	Header       http.Header
	Body         []byte
}

// args decodes a JSON request body (numbers as json.Number).
func (c call) args(t testing.TB) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(c.Body))
	dec.UseNumber()
	var m map[string]any
	require.NoError(t, dec.Decode(&m))
	return m
}

type handler func(c call) (int, []byte)

// fakeERP routes requests by "METHOD /decoded/path" and records every call.
type fakeERP struct {
	t      testing.TB
	mu     sync.Mutex
	routes map[string]handler
	calls  []call
}

func newFakeERP(t testing.TB) *fakeERP {
	f := &fakeERP{t: t, routes: map[string]handler{}}
	f.on("GET /api/resource/Company", fixedReply(200, fixture(t, "company_list.json")))
	f.on("GET /api/method/frappe.desk.form.load.getdoctype", func(c call) (int, []byte) {
		switch {
		case strings.Contains(c.Query, "doctype=Sales+Order"):
			return 200, fixture(t, "sales_order_meta.json")
		case strings.Contains(c.Query, "doctype=Delivery+Note"),
			strings.Contains(c.Query, "doctype=Sales+Invoice"),
			strings.Contains(c.Query, "doctype=Payment+Entry"):
			return 200, fixture(t, "delivery_note_meta.json")
		}
		return 404, []byte(`{"exc_type":"DoesNotExistError"}`)
	})
	return f
}

func fixedReply(status int, body []byte) handler {
	return func(call) (int, []byte) { return status, body }
}

func (f *fakeERP) on(route string, h handler) { f.routes[route] = h }

func (f *fakeERP) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	c := call{Ctx: r.Context(), Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Header: r.Header.Clone(), Body: body}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	h, ok := f.routes[r.Method+" "+r.URL.Path]
	f.mu.Unlock()
	status, out := 404, []byte(`{"exc_type":"DoesNotExistError","exception":"no route `+r.Method+" "+r.URL.Path+`"}`)
	if ok {
		status, out = h(c)
	}
	if err := r.Context().Err(); err != nil {
		return nil, err // like a real transport: the request was abandoned
	}
	return resp(status, string(out)), nil
}

func (f *fakeERP) callsTo(route string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Method+" "+c.Path == route {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeERP) adapter() *Adapter { return NewAdapter(newTestClient(f)) }

func TestMapSalesOrderExactDecimals(t *testing.T) {
	f := newFakeERP(t)
	f.on("GET "+pathSO, fixedReply(200, fixture(t, "sales_order.json")))

	so, err := f.adapter().GetSalesOrder(context.Background(), sales, soName)
	require.NoError(t, err)
	assert.Equal(t, erp.DocRef{DocType: erp.DocSalesOrder, Name: soName}, so.Ref)
	assert.Equal(t, "ACME-TH", so.Company)
	assert.Equal(t, "CUST-001", so.Customer)
	assert.Equal(t, erp.MustDate("2026-10-09"), so.TransactionDate)
	assert.Equal(t, erp.MustDate("2026-10-09"), so.DeliveryDate)
	assert.Equal(t, "1234567.89", so.GrandTotal.String())
	assert.Equal(t, "1153801.77", so.NetTotal.String())
	assert.Equal(t, "80766.12", so.TaxTotal.String())
	assert.Equal(t, "1234568.0", so.RoundedTotal.String())
	require.Len(t, so.Lines, 2)
	assert.Equal(t, "ITEM-002", so.Lines[0].ItemCode)
	assert.Equal(t, "0.1", so.Lines[0].Rate.String())
	assert.Equal(t, "135.0", so.Lines[0].Qty.String())
	assert.Equal(t, "13.5", so.Lines[0].Amount.String())
	assert.Equal(t, "1153788.27", so.Lines[1].Amount.String())

	reqs := f.callsTo("GET " + pathSO)
	require.Len(t, reqs, 1)
	assert.Equal(t, "sid=sess-sales", reqs[0].Header.Get("Cookie"), "the caller's own credential")
}

func TestMapStatusAndWorkflowState(t *testing.T) {
	cases := []struct {
		docstatus int
		state     string
		want      erp.DocStatus
	}{
		{0, "Draft", erp.DocDraft},
		{0, "รออนุมัติ", erp.DocDraft},
		{1, "อนุมัติแล้ว", erp.DocSubmitted},
		{2, "อนุมัติแล้ว", erp.DocCancelled},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%d %s", c.docstatus, c.state), func(t *testing.T) {
			f := newFakeERP(t)
			doc := patchDoc(t, fixture(t, "sales_order.json"), "data",
				map[string]any{"docstatus": c.docstatus, "workflow_state": c.state})
			f.on("GET "+pathSO, fixedReply(200, doc))
			so, err := f.adapter().GetSalesOrder(context.Background(), sales, soName)
			require.NoError(t, err)
			assert.Equal(t, c.want, so.Status)
			assert.Equal(t, c.state, so.WorkflowState)
		})
	}

	t.Run("unknown docstatus", func(t *testing.T) {
		f := newFakeERP(t)
		f.on("GET "+pathSO, fixedReply(200, patchDoc(t, fixture(t, "sales_order.json"), "data", map[string]any{"docstatus": 7})))
		_, err := f.adapter().GetSalesOrder(context.Background(), sales, soName)
		require.Error(t, err)
	})
}

func TestRoundedTotalIsGrandTotalWhenRoundingIsDisabled(t *testing.T) {
	f := newFakeERP(t)
	doc := patchDoc(t, fixture(t, "sales_order.json"), "data",
		map[string]any{"disable_rounded_total": 1, "rounded_total": 0, "rounding_adjustment": 0})
	f.on("GET "+pathSO, fixedReply(200, doc))
	so, err := f.adapter().GetSalesOrder(context.Background(), sales, soName)
	require.NoError(t, err)
	assert.Equal(t, "1234567.89", so.RoundedTotal.String())
}

// salesOrderRoutes answers the calls CreateSalesOrder makes with the captured responses.
func salesOrderRoutes(t *testing.T, f *fakeERP) {
	f.on("POST "+pathMethod+"erpnext.accounts.party.get_party_details", fixedReply(200, fixture(t, "party_details.json")))
	f.on("POST "+pathMethod+"erpnext.controllers.accounts_controller.get_default_taxes_and_charges",
		fixedReply(200, fixture(t, "default_taxes.json")))
	f.on("POST /api/resource/Sales Order", fixedReply(200, fixture(t, "sales_order.json")))
}

func orderInput(company string) erp.SalesOrderInput {
	return erp.SalesOrderInput{
		Company: company, Customer: "CUST-001",
		TransactionDate: erp.MustDate("2026-10-09"), DeliveryDate: erp.MustDate("2026-10-16"),
		Lines: []erp.LineInput{
			{ItemCode: "ITEM-002", Qty: erp.MustDecimal("3")},
			{ItemCode: "ITEM-018", Qty: erp.MustDecimal("1.5"), Rate: new(erp.MustDecimal("4499.99"))},
		},
	}
}

func TestCompanyAbbrMapping(t *testing.T) {
	f := newFakeERP(t)
	salesOrderRoutes(t, f)
	f.on("GET "+pathSO, fixedReply(200, fixture(t, "sales_order.json")))
	a := f.adapter()
	ctx := context.Background()

	ref, err := a.CreateSalesOrder(ctx, sales, orderInput("ACME-TH"))
	require.NoError(t, err)
	assert.Equal(t, erp.DocRef{DocType: erp.DocSalesOrder, Name: soName}, ref)

	party := f.callsTo("POST " + pathMethod + "erpnext.accounts.party.get_party_details")
	require.Len(t, party, 1)
	assert.Equal(t, map[string]any{
		"party": "CUST-001", "party_type": "Customer", "company": companyName,
		"doctype": "Sales Order", "posting_date": "2026-10-09",
	}, party[0].args(t))
	taxes := f.callsTo("POST " + pathMethod + "erpnext.controllers.accounts_controller.get_default_taxes_and_charges")
	require.Len(t, taxes, 1)
	assert.Equal(t, map[string]any{"master_doctype": "Sales Taxes and Charges Template", "company": companyName}, taxes[0].args(t))

	inserts := f.callsTo("POST /api/resource/Sales Order")
	require.Len(t, inserts, 1)
	body := inserts[0].args(t)
	assert.Equal(t, companyName, body["company"])
	assert.Equal(t, "CUST-001", body["customer"])
	assert.Equal(t, "Sales", body["order_type"])
	assert.Equal(t, "2026-10-09", body["transaction_date"])
	assert.Equal(t, "2026-10-16", body["delivery_date"])
	assert.Equal(t, "ขายมาตรฐาน", body["selling_price_list"], "the customer's price list, as Desk fills it")
	assert.Equal(t, "ภาษีขาย 7% - ACME-TH", body["taxes_and_charges"])
	rows, ok := body["taxes"].([]any)
	require.True(t, ok, "tax rows of the company's default template are sent along")
	require.Len(t, rows, 1)
	assert.Equal(t, "2120 - ภาษีขาย - ACME-TH", rows[0].(map[string]any)["account_head"])
	assert.Equal(t, []any{
		map[string]any{"item_code": "ITEM-002", "qty": json.Number("3")},
		map[string]any{"item_code": "ITEM-018", "qty": json.Number("1.5"), "rate": json.Number("4499.99")},
	}, body["items"], "quantities and rates go out as exact JSON numbers")

	so, err := a.GetSalesOrder(ctx, sales, ref.Name)
	require.NoError(t, err)
	assert.Equal(t, "ACME-TH", so.Company)
	assert.Len(t, f.callsTo("GET /api/resource/Company"), 1, "the company list is cached")
}

func TestUnknownCompanyReloadsOnceThenIsValidation(t *testing.T) {
	f := newFakeERP(t)
	salesOrderRoutes(t, f)
	a := f.adapter()
	_, err := a.CreateSalesOrder(context.Background(), sales, orderInput("ACME-TH"))
	require.NoError(t, err)
	require.Len(t, f.callsTo("GET /api/resource/Company"), 1)

	_, err = a.CreateSalesOrder(context.Background(), sales, orderInput("NOPE"))
	var ve *erp.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "company")
	assert.Len(t, f.callsTo("GET /api/resource/Company"), 2, "a miss reloads the cached list once")
	assert.Len(t, f.callsTo("POST /api/resource/Sales Order"), 1)
}

func TestUnknownCustomerIsValidation(t *testing.T) {
	f := newFakeERP(t)
	salesOrderRoutes(t, f)
	f.on("POST "+pathMethod+"erpnext.accounts.party.get_party_details", fixedReply(404, fixture(t, "customer_not_found.json")))
	in := orderInput("ACME-TH")
	in.Customer = "CUST-NOPE"
	_, err := f.adapter().CreateSalesOrder(context.Background(), sales, in)
	var ve *erp.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "customer")
	assert.Contains(t, ve.Fields["customer"], "CUST-NOPE")
	assert.Empty(t, f.callsTo("POST /api/resource/Sales Order"))
}

func TestCreateSalesOrderForbiddenPassesThrough(t *testing.T) {
	f := newFakeERP(t)
	salesOrderRoutes(t, f)
	f.on("POST /api/resource/Sales Order", fixedReply(403, []byte(`{"exc_type":"PermissionError"}`)))
	_, err := f.adapter().CreateSalesOrder(context.Background(), sales, orderInput("ACME-TH"))
	require.ErrorIs(t, err, erp.ErrForbidden)
}

// workflowRoutes serves the Sales Order, the given transitions and an apply_workflow that moves
// the document to the chosen transition's next state (docstatus from the workflow fixture).
func workflowRoutes(t *testing.T, f *fakeERP, transitions []map[string]string) {
	submitted := map[string]bool{"อนุมัติแล้ว": true}
	f.on("GET "+pathSO, fixedReply(200, fixture(t, "sales_order.json")))
	f.on("POST "+pathMethod+"frappe.model.workflow.get_transitions", func(call) (int, []byte) {
		b, _ := json.Marshal(map[string]any{"message": transitions})
		return 200, b
	})
	f.on("POST "+pathMethod+"frappe.model.workflow.apply_workflow", func(c call) (int, []byte) {
		action, _ := c.args(t)["action"].(string)
		for _, tr := range transitions {
			if tr["action"] == action {
				ds := 0
				if submitted[tr["next_state"]] {
					ds = 1
				}
				return 200, patchDoc(t, fixture(t, "sales_order.json"), "message",
					map[string]any{"docstatus": ds, "workflow_state": tr["next_state"]})
			}
		}
		return 417, []byte(`{"exc_type":"WorkflowTransitionError"}`)
	})
}

func tr(from, action, to string) map[string]string {
	return map[string]string{"state": from, "action": action, "next_state": to}
}

func TestSubmitChoosesTransition(t *testing.T) {
	cases := []struct {
		name        string
		transitions []map[string]string
		wantAction  string
		want        erp.SubmitResult
		wantErr     bool
	}{
		{
			"single submitting",
			[]map[string]string{tr("Draft", "ยืนยัน", "อนุมัติแล้ว")},
			"ยืนยัน",
			erp.SubmitResult{Submitted: true, WorkflowState: "อนุมัติแล้ว"},
			false,
		},
		{
			"single approval request",
			[]map[string]string{tr("Draft", "ขออนุมัติ", "รออนุมัติ")},
			"ขออนุมัติ",
			erp.SubmitResult{Submitted: false, WorkflowState: "รออนุมัติ"},
			false,
		},
		{
			"submitting one among several",
			[]map[string]string{tr("รออนุมัติ", "ปฏิเสธ", "ปฏิเสธ"), tr("รออนุมัติ", "อนุมัติ", "อนุมัติแล้ว")},
			"อนุมัติ",
			erp.SubmitResult{Submitted: true, WorkflowState: "อนุมัติแล้ว"},
			false,
		},
		{"two non-submitting", []map[string]string{tr("Draft", "ขออนุมัติ", "รออนุมัติ"), tr("Draft", "ปฏิเสธ", "ปฏิเสธ")}, "", erp.SubmitResult{}, true},
		{"none", nil, "", erp.SubmitResult{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeERP(t)
			workflowRoutes(t, f, c.transitions)
			res, err := f.adapter().Submit(context.Background(), sales, erp.DocRef{DocType: erp.DocSalesOrder, Name: soName})
			applied := f.callsTo("POST " + pathMethod + "frappe.model.workflow.apply_workflow")
			if c.wantErr {
				var ve *erp.ValidationError
				require.ErrorAs(t, err, &ve)
				assert.Contains(t, ve.Fields, "workflow")
				assert.Empty(t, applied)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, res)
			require.Len(t, applied, 1)
			args := applied[0].args(t)
			assert.Equal(t, c.wantAction, args["action"])
			doc, ok := args["doc"].(map[string]any)
			require.True(t, ok, "apply_workflow gets the document")
			assert.Equal(t, soName, doc["name"])
			assert.Empty(t, f.callsTo("POST "+pathMethod+"frappe.client.submit"))
		})
	}
}

// capturedTransitions reads a get_transitions answer captured from test.localhost.
func capturedTransitions(t *testing.T, name string) []map[string]string {
	var env struct {
		Message []map[string]any `json:"message"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, name), &env))
	out := make([]map[string]string, 0, len(env.Message))
	for _, m := range env.Message {
		out = append(out, tr(m["state"].(string), m["action"].(string), m["next_state"].(string)))
	}
	return out
}

func TestSubmitWithCapturedTransitions(t *testing.T) {
	cases := []struct {
		fixture, wantAction string
		want                erp.SubmitResult
	}{
		{"transitions_draft.json", "ยืนยัน", erp.SubmitResult{Submitted: true, WorkflowState: "อนุมัติแล้ว"}},
		{"transitions_over_credit.json", "ขออนุมัติ", erp.SubmitResult{Submitted: false, WorkflowState: "รออนุมัติ"}},
		{"transitions_pending_approver.json", "อนุมัติ", erp.SubmitResult{Submitted: true, WorkflowState: "อนุมัติแล้ว"}},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			f := newFakeERP(t)
			workflowRoutes(t, f, capturedTransitions(t, c.fixture))
			f.on("POST "+pathMethod+"frappe.model.workflow.get_transitions", fixedReply(200, fixture(t, c.fixture)))
			res, err := f.adapter().Submit(context.Background(), sales, erp.DocRef{DocType: erp.DocSalesOrder, Name: soName})
			require.NoError(t, err)
			assert.Equal(t, c.want, res)
			applied := f.callsTo("POST " + pathMethod + "frappe.model.workflow.apply_workflow")
			require.Len(t, applied, 1)
			assert.Equal(t, c.wantAction, applied[0].args(t)["action"])
		})
	}
}

func TestSubmitRejectsNonDraft(t *testing.T) {
	f := newFakeERP(t)
	workflowRoutes(t, f, []map[string]string{tr("Draft", "ยืนยัน", "อนุมัติแล้ว")})
	f.on("GET "+pathSO, fixedReply(200, patchDoc(t, fixture(t, "sales_order.json"), "data", map[string]any{"docstatus": 1})))
	_, err := f.adapter().Submit(context.Background(), sales, erp.DocRef{DocType: erp.DocSalesOrder, Name: soName})
	require.ErrorIs(t, err, erp.ErrConflict)
	assert.Empty(t, f.callsTo("POST "+pathMethod+"frappe.model.workflow.apply_workflow"))
}

func TestSubmitWithoutWorkflowUsesClientSubmit(t *testing.T) {
	f := newFakeERP(t)
	const path = "/api/resource/Delivery Note/DN6910-0001"
	f.on("GET "+path, fixedReply(200, patchDoc(t, fixture(t, "delivery_note.json"), "data", map[string]any{"docstatus": 0})))
	f.on("POST "+pathMethod+"frappe.client.submit", fixedReply(200, fixture(t, "delivery_note.json")))

	res, err := f.adapter().Submit(context.Background(), sales, erp.DocRef{DocType: erp.DocDeliveryNote, Name: "DN6910-0001"})
	require.NoError(t, err)
	assert.Equal(t, erp.SubmitResult{Submitted: true}, res)
	submits := f.callsTo("POST " + pathMethod + "frappe.client.submit")
	require.Len(t, submits, 1)
	doc, ok := submits[0].args(t)["doc"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "DN6910-0001", doc["name"])
	assert.Empty(t, f.callsTo("POST "+pathMethod+"frappe.model.workflow.get_transitions"))
}

func TestActions(t *testing.T) {
	t.Run("workflow", func(t *testing.T) {
		f := newFakeERP(t)
		workflowRoutes(t, f, []map[string]string{tr("รออนุมัติ", "อนุมัติ", "อนุมัติแล้ว"), tr("รออนุมัติ", "ปฏิเสธ", "ปฏิเสธ")})
		got, err := f.adapter().Actions(context.Background(), sales, erp.DocRef{DocType: erp.DocSalesOrder, Name: soName})
		require.NoError(t, err)
		assert.Equal(t, []string{"อนุมัติ", "ปฏิเสธ"}, got)
	})
	t.Run("no workflow", func(t *testing.T) {
		f := newFakeERP(t)
		f.on("GET /api/resource/Delivery Note/DN6910-0001", fixedReply(200, fixture(t, "delivery_note.json")))
		got, err := f.adapter().Actions(context.Background(), sales, erp.DocRef{DocType: erp.DocDeliveryNote, Name: "DN6910-0001"})
		require.NoError(t, err)
		assert.Equal(t, []string{}, got)
		assert.Empty(t, f.callsTo("POST "+pathMethod+"frappe.model.workflow.get_transitions"))
	})
	t.Run("missing document", func(t *testing.T) {
		f := newFakeERP(t)
		_, err := f.adapter().Actions(context.Background(), sales, erp.DocRef{DocType: erp.DocDeliveryNote, Name: "DN-NOPE"})
		require.ErrorIs(t, err, erp.ErrNotFound)
	})
}

func TestApply(t *testing.T) {
	f := newFakeERP(t)
	workflowRoutes(t, f, []map[string]string{tr("รออนุมัติ", "อนุมัติ", "อนุมัติแล้ว")})
	a := f.adapter()
	ref := erp.DocRef{DocType: erp.DocSalesOrder, Name: soName}
	res, err := a.Apply(context.Background(), sales, ref, "อนุมัติ")
	require.NoError(t, err)
	assert.Equal(t, erp.SubmitResult{Submitted: true, WorkflowState: "อนุมัติแล้ว"}, res)

	_, err = a.Apply(context.Background(), sales, ref, "ไม่มี")
	require.ErrorIs(t, err, erp.ErrValidation, "ERPNext's WorkflowTransitionError")

	_, err = a.Apply(context.Background(), sales, erp.DocRef{DocType: erp.DocDeliveryNote, Name: "DN6910-0001"}, "อนุมัติ")
	var ve *erp.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "workflow")
}

func TestWorkflowMetaIsCached(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeERP(t)
		workflowRoutes(t, f, []map[string]string{tr("Draft", "ยืนยัน", "อนุมัติแล้ว")})
		a := f.adapter()
		ref := erp.DocRef{DocType: erp.DocSalesOrder, Name: soName}
		for range 2 {
			_, err := a.Actions(context.Background(), sales, ref)
			require.NoError(t, err)
		}
		assert.Len(t, f.callsTo("GET "+pathMethod+"frappe.desk.form.load.getdoctype"), 1)
		time.Sleep(workflowTTL + time.Second)
		_, err := a.Actions(context.Background(), sales, ref)
		require.NoError(t, err)
		assert.Len(t, f.callsTo("GET "+pathMethod+"frappe.desk.form.load.getdoctype"), 2, "refreshed after the TTL")
	})
}

func TestCancel(t *testing.T) {
	ref := erp.DocRef{DocType: erp.DocSalesOrder, Name: soName}
	linked := []byte(`{"exc_type":"LinkExistsError","_server_messages":"[\"{\\\"message\\\":\\\"linked with DN6910-0001\\\"}\"]"}`)
	cases := []struct {
		name       string
		docstatus  int
		cancel     handler
		target     error
		wantCancel bool
	}{
		{"submitted", 1, fixedReply(200, nil), nil, true},
		{"already cancelled", 2, nil, erp.ErrConflict, false},
		{"draft", 0, nil, erp.ErrValidation, false},
		{"linked submitted documents", 1, fixedReply(417, linked), erp.ErrConflict, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeERP(t)
			f.on("GET "+pathSO, fixedReply(200, patchDoc(t, fixture(t, "sales_order.json"), "data", map[string]any{"docstatus": c.docstatus})))
			cancelled := patchDoc(t, fixture(t, "sales_order.json"), "message", map[string]any{"docstatus": 2})
			if c.cancel != nil {
				f.on("POST "+pathMethod+"frappe.client.cancel", func(cl call) (int, []byte) {
					status, body := c.cancel(cl)
					if body == nil {
						body = cancelled
					}
					return status, body
				})
			}
			err := f.adapter().Cancel(context.Background(), sales, ref)
			cancels := f.callsTo("POST " + pathMethod + "frappe.client.cancel")
			if c.wantCancel {
				require.Len(t, cancels, 1)
				assert.Equal(t, map[string]any{"doctype": "Sales Order", "name": soName}, cancels[0].args(t))
			} else {
				assert.Empty(t, cancels, "refused before calling ERPNext")
			}
			if c.target == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, c.target)
			if c.target == erp.ErrConflict {
				assert.NotErrorIs(t, err, erp.ErrValidation)
			}
		})
	}

	t.Run("missing document", func(t *testing.T) {
		f := newFakeERP(t)
		err := f.adapter().Cancel(context.Background(), sales, erp.DocRef{DocType: erp.DocSalesOrder, Name: "SO-NOPE"})
		require.ErrorIs(t, err, erp.ErrNotFound)
		assert.Empty(t, f.callsTo("POST "+pathMethod+"frappe.client.cancel"))
	})
}

func TestCreateDeliveryNote(t *testing.T) {
	draft := []byte(`{"message":{"doctype":"Delivery Note","company":"` + companyName + `","customer":"CUST-001",` +
		`"posting_date":"2026-10-09","set_posting_time":0,"items":[{"item_code":"ITEM-002","qty":3.0,"rate":12.0,"against_sales_order":"SO6910-0001"}]}}`)
	f := newFakeERP(t)
	f.on("POST "+pathMethod+"erpnext.selling.doctype.sales_order.sales_order.make_delivery_note", fixedReply(200, draft))
	f.on("POST /api/resource/Delivery Note", fixedReply(200, fixture(t, "delivery_note.json")))
	a := f.adapter()
	so := erp.DocRef{DocType: erp.DocSalesOrder, Name: soName}

	ref, err := a.CreateDeliveryNote(context.Background(), sales, so, nil)
	require.NoError(t, err)
	assert.Equal(t, erp.DocRef{DocType: erp.DocDeliveryNote, Name: "DN6910-0001"}, ref)
	makes := f.callsTo("POST " + pathMethod + "erpnext.selling.doctype.sales_order.sales_order.make_delivery_note")
	require.Len(t, makes, 1)
	assert.Equal(t, map[string]any{"source_name": soName}, makes[0].args(t))
	inserted := f.callsTo("POST /api/resource/Delivery Note")[0].args(t)
	assert.Equal(t, json.Number("0"), inserted["set_posting_time"])
	assert.Equal(t, json.Number("3.0"), inserted["items"].([]any)[0].(map[string]any)["qty"], "the draft goes back unchanged")

	_, err = a.CreateDeliveryNote(context.Background(), sales, so, new(erp.MustDate("2026-10-01")))
	require.NoError(t, err)
	inserted = f.callsTo("POST /api/resource/Delivery Note")[1].args(t)
	assert.Equal(t, json.Number("1"), inserted["set_posting_time"])
	assert.Equal(t, "2026-10-01", inserted["posting_date"])

	_, err = a.CreateDeliveryNote(context.Background(), sales, erp.DocRef{DocType: erp.DocSalesInvoice, Name: "x"}, nil)
	require.ErrorIs(t, err, erp.ErrValidation)
}

func TestCreateSalesInvoice(t *testing.T) {
	draft := []byte(`{"message":{"doctype":"Sales Invoice","company":"` + companyName + `","customer":"CUST-001",` +
		`"posting_date":"2026-10-09","due_date":"2026-11-08","set_posting_time":0,` +
		`"payment_schedule":[{"due_date":"2026-11-08","payment_amount":39.0}],"items":[{"item_code":"ITEM-002","qty":3.0}]}}`)
	f := newFakeERP(t)
	f.on("POST "+pathMethod+"erpnext.stock.doctype.delivery_note.delivery_note.make_sales_invoice", fixedReply(200, draft))
	f.on("POST /api/resource/Sales Invoice", fixedReply(200, fixture(t, "sales_invoice.json")))
	a := f.adapter()
	dn := erp.DocRef{DocType: erp.DocDeliveryNote, Name: "DN6910-0001"}

	ref, err := a.CreateSalesInvoice(context.Background(), sales, dn, nil)
	require.NoError(t, err)
	assert.Equal(t, erp.DocRef{DocType: erp.DocSalesInvoice, Name: "IV6910-0001"}, ref)
	makes := f.callsTo("POST " + pathMethod + "erpnext.stock.doctype.delivery_note.delivery_note.make_sales_invoice")
	assert.Equal(t, map[string]any{"source_name": "DN6910-0001"}, makes[0].args(t))
	inserted := f.callsTo("POST /api/resource/Sales Invoice")[0].args(t)
	assert.Equal(t, "2026-11-08", inserted["due_date"])

	_, err = a.CreateSalesInvoice(context.Background(), sales, dn, new(erp.MustDate("2026-10-01")))
	require.NoError(t, err)
	inserted = f.callsTo("POST /api/resource/Sales Invoice")[1].args(t)
	assert.Equal(t, json.Number("1"), inserted["set_posting_time"])
	assert.Equal(t, "2026-10-01", inserted["posting_date"])
	assert.Equal(t, "2026-10-31", inserted["due_date"], "the credit days are kept")
	assert.Equal(t, []any{}, inserted["payment_schedule"], "ERPNext rebuilds the schedule")

	_, err = a.CreateSalesInvoice(context.Background(), sales, erp.DocRef{DocType: erp.DocSalesOrder, Name: soName}, nil)
	require.ErrorIs(t, err, erp.ErrValidation)
}

func TestCreatePayment(t *testing.T) {
	f := newFakeERP(t)
	f.on("POST "+pathMethod+"erpnext.accounts.doctype.payment_entry.payment_entry.get_payment_entry", fixedReply(200, fixture(t, "payment_entry_draft.json")))
	f.on("POST "+pathMethod+"erpnext.accounts.doctype.sales_invoice.sales_invoice.get_bank_cash_account", fixedReply(200, fixture(t, "bank_cash_account.json")))
	f.on("POST /api/resource/Payment Entry", fixedReply(200, fixture(t, "payment_entry.json")))
	a := f.adapter()

	ref, err := a.CreatePayment(context.Background(), sales, erp.PaymentInput{Invoice: "IV6910-0001", ModeOfPayment: "เงินสด"})
	require.NoError(t, err)
	assert.Equal(t, erp.DocRef{DocType: erp.DocPaymentEntry, Name: "RC6910-0001"}, ref)

	draftCall := f.callsTo("POST " + pathMethod + "erpnext.accounts.doctype.payment_entry.payment_entry.get_payment_entry")
	assert.Equal(t, map[string]any{"dt": "Sales Invoice", "dn": "IV6910-0001"}, draftCall[0].args(t))
	accCall := f.callsTo("POST " + pathMethod + "erpnext.accounts.doctype.sales_invoice.sales_invoice.get_bank_cash_account")
	assert.Equal(t, map[string]any{"mode_of_payment": "เงินสด", "company": companyName}, accCall[0].args(t))
	body := f.callsTo("POST /api/resource/Payment Entry")[0].args(t)
	assert.Equal(t, "เงินสด", body["mode_of_payment"])
	assert.Equal(t, "1110 - เงินสด - ACME-TH", body["paid_to"], "received into the mode of payment's account")
	assert.Equal(t, "1130 - ลูกหนี้การค้า - ACME-TH", body["paid_from"])
	assert.Equal(t, "IV6910-0001", body["reference_no"])
	assert.Equal(t, "2026-10-09", body["reference_date"])
	assert.Equal(t, json.Number("39.0"), body["paid_amount"])

	_, err = a.CreatePayment(context.Background(), sales, erp.PaymentInput{
		Invoice: "IV6910-0001", ModeOfPayment: "เงินสด", PostingDate: new(erp.MustDate("2026-10-01")),
	})
	require.NoError(t, err)
	body = f.callsTo("POST /api/resource/Payment Entry")[1].args(t)
	assert.Equal(t, "2026-10-01", body["posting_date"])
	assert.Equal(t, "2026-10-01", body["reference_date"])

	_, err = a.CreatePayment(context.Background(), sales, erp.PaymentInput{Invoice: "IV6910-0001"})
	var ve *erp.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "mode_of_payment")
}

func TestMapDeliveryNoteInvoiceAndPayment(t *testing.T) {
	f := newFakeERP(t)
	f.on("GET /api/resource/Delivery Note/DN6910-0001", fixedReply(200, fixture(t, "delivery_note.json")))
	f.on("GET /api/resource/Sales Invoice/IV6910-0001", fixedReply(200, fixture(t, "sales_invoice.json")))
	f.on("GET /api/resource/Payment Entry/RC6910-0001", fixedReply(200, fixture(t, "payment_entry.json")))
	a := f.adapter()
	ctx := context.Background()

	dn, err := a.GetDeliveryNote(ctx, sales, "DN6910-0001")
	require.NoError(t, err)
	assert.Equal(t, erp.DocRef{DocType: erp.DocDeliveryNote, Name: "DN6910-0001"}, dn.Ref)
	assert.Equal(t, "ACME-TH", dn.Company)
	assert.Equal(t, "CUST-001", dn.Customer)
	assert.Equal(t, erp.DocSubmitted, dn.Status)
	assert.Equal(t, erp.MustDate("2026-10-09"), dn.PostingDate)
	assert.Equal(t, []string{soName}, dn.AgainstSalesOrders)
	require.Len(t, dn.Lines, 1)
	assert.Equal(t, "36.0", dn.Lines[0].Amount.String())

	si, err := a.GetSalesInvoice(ctx, sales, "IV6910-0001")
	require.NoError(t, err)
	assert.Equal(t, "ACME-TH", si.Company)
	assert.Equal(t, erp.DocSubmitted, si.Status)
	assert.Equal(t, []string{"DN6910-0001"}, si.AgainstDeliveryNotes)
	assert.False(t, si.IsReturn)
	assert.Equal(t, "36.0", si.NetTotal.String())
	assert.Equal(t, "2.52", si.TaxTotal.String())
	assert.Equal(t, "38.52", si.GrandTotal.String())
	assert.Equal(t, "39.0", si.RoundedTotal.String())
	assert.Equal(t, "0.0", si.Outstanding.String())
	assert.Equal(t, erp.MustDate("2026-10-09"), si.DueDate)

	pe, err := a.GetPaymentEntry(ctx, sales, "RC6910-0001")
	require.NoError(t, err)
	assert.Equal(t, "ACME-TH", pe.Company)
	assert.Equal(t, "CUST-001", pe.Party)
	assert.Equal(t, "39.0", pe.Paid.String())
	assert.Equal(t, "เงินสด", pe.ModeOfPayment)
	assert.Equal(t, erp.DocSubmitted, pe.Status)
	assert.Equal(t, []string{"IV6910-0001"}, pe.AgainstInvoices)
}

func TestSalesInvoiceLineageRetainsAllDistinctAuthorizedSources(t *testing.T) {
	for _, names := range [][]string{{"DN-a", "DN-b", "DN-a", ""}, {""}} {
		t.Run(strings.Join(names, ","), func(t *testing.T) {
			var payload map[string]any
			require.NoError(t, json.Unmarshal(fixture(t, "sales_invoice.json"), &payload))
			data := payload["data"].(map[string]any)
			source := data["items"].([]any)[0].(map[string]any)
			items := []any{}
			expected := []string{}
			for _, name := range names {
				row := map[string]any{}
				for k, v := range source {
					row[k] = v
				}
				row["delivery_note"] = name
				items = append(items, row)
				if name != "" && !slices.Contains(expected, name) {
					expected = append(expected, name)
				}
			}
			data["items"] = items
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			f := newFakeERP(t)
			f.on("GET /api/resource/Sales Invoice/IV6910-0001", fixedReply(200, body))
			invoice, err := f.adapter().GetSalesInvoice(t.Context(), sales, "IV6910-0001")
			require.NoError(t, err)
			assert.Equal(t, expected, invoice.AgainstDeliveryNotes)
			assert.Len(t, invoice.Lines, len(names), "lineage deduplication must not discard invoice items")
		})
	}
}

func TestWhoami(t *testing.T) {
	const path = pathMethod + "erp_town.session_api.whoami"
	t.Run("session", func(t *testing.T) {
		f := newFakeERP(t)
		f.on("GET "+path, fixedReply(200, fixture(t, "whoami.json")))
		id, err := f.adapter().Whoami(context.Background(), erp.SessionCredential{SessionID: "sess-sales"})
		require.NoError(t, err)
		assert.Equal(t, "sales@acme-th.test", id.User)
		assert.Equal(t, []string{"Sales User"}, id.Roles, "Frappe's automatic roles are dropped")
		assert.Equal(t, []string{"ACME-TH", "ETT"}, id.Companies)
		assert.Equal(t, erp.SessionCredential{SessionID: "sess-sales", CSRFToken: "csrf-token-of-the-session"}, id.Credential)
		calls := f.callsTo("GET " + path)
		require.Len(t, calls, 1, "a GET: Frappe checks no CSRF token on it")
		assert.Equal(t, "sid=sess-sales", calls[0].Header.Get("Cookie"))
	})
	t.Run("api key", func(t *testing.T) {
		f := newFakeERP(t)
		f.on("GET "+path, fixedReply(200, fixture(t, "whoami.json")))
		cred := erp.APIKeyCredential{Key: "k", Secret: "s"}
		id, err := f.adapter().Whoami(context.Background(), cred)
		require.NoError(t, err)
		assert.Equal(t, cred, id.Credential)
	})
	for _, status := range []int{401, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			f := newFakeERP(t)
			f.on("GET "+path, fixedReply(status, []byte(`{"exc_type":"SessionExpired"}`)))
			_, err := f.adapter().Whoami(context.Background(), erp.SessionCredential{SessionID: "gone"})
			require.ErrorIs(t, err, erp.ErrUnauthenticated)
			assert.NotErrorIs(t, err, erp.ErrForbidden)
		})
	}
}

func FuzzMapSalesOrder(f *testing.F) {
	f.Add(fixture(f, "sales_order.json"))
	f.Add([]byte(`{"data":{"docstatus":1,"items":[{"qty":1e-05}]}}`))
	f.Add([]byte(`{"data":{"grand_total":"x"}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(raw, &env) != nil {
			return
		}
		var doc salesOrderDoc
		if decodeJSON(env.Data, &doc) != nil {
			return
		}
		_, _ = mapSalesOrder(doc, func(name string) (string, error) { return name, nil })
	})
}

func principalFor(sess string) erp.Principal {
	return erp.Principal{Kind: erp.PrincipalUser, ID: sess + "@acme-th.test", Credential: erp.SessionCredential{SessionID: sess}}
}

// companyRoute answers the Company list after delay with the rows visible to
// each session (views: sid -> rows JSON); sessions not listed see the fixture.
func companyRoute(t *testing.T, f *fakeERP, delay time.Duration, status int, views map[string]string) {
	f.on("GET /api/resource/Company", func(c call) (int, []byte) {
		select {
		case <-time.After(delay):
		case <-c.Ctx.Done():
			return 0, nil
		}
		if status != 200 {
			return status, []byte(`{}`)
		}
		sid := strings.TrimPrefix(c.Header.Get("Cookie"), "sid=")
		if rows, ok := views[sid]; ok {
			return 200, []byte(`{"data":` + rows + `}`)
		}
		return 200, fixture(t, "company_list.json")
	})
}

func TestConcurrentCompanyLookupsShareOneLoad(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeERP(t)
		companyRoute(t, f, 2*time.Second, 200, nil)
		a := f.adapter()
		var wg sync.WaitGroup
		for i := range 5 {
			wg.Go(func() {
				name, err := a.companyName(context.Background(), principalFor(fmt.Sprintf("s%d", i)), "ACME-TH")
				assert.NoError(t, err)
				assert.Equal(t, companyName, name)
			})
		}
		wg.Wait()
		assert.Len(t, f.callsTo("GET /api/resource/Company"), 1)
	})
}

func TestCompanyViewsAreMerged(t *testing.T) {
	f := newFakeERP(t)
	companyRoute(t, f, 0, 200, map[string]string{
		"sess-a": `[{"name":"` + companyName + `","abbr":"ACME-TH"}]`,
		"sess-b": `[{"name":"ERP Town Test Company","abbr":"ETT"}]`,
	})
	a := f.adapter()
	ctx := context.Background()
	userA, userB := principalFor("sess-a"), principalFor("sess-b")

	name, err := a.companyName(ctx, userA, "ACME-TH")
	require.NoError(t, err)
	assert.Equal(t, companyName, name)
	name, err = a.companyName(ctx, userB, "ETT")
	require.NoError(t, err)
	assert.Equal(t, "ERP Town Test Company", name)

	for range 3 {
		_, err = a.companyName(ctx, userA, "ACME-TH")
		require.NoError(t, err)
		_, err = a.companyName(ctx, userB, "ETT")
		require.NoError(t, err)
	}
	abbr, err := a.companyAbbr(ctx, userA)("ERP Town Test Company")
	require.NoError(t, err)
	assert.Equal(t, "ETT", abbr)
	assert.Len(t, f.callsTo("GET /api/resource/Company"), 2, "one load per view, then everything from the merged cache")
}

func TestUnknownCompanyReloadIsRateLimited(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeERP(t)
		a := f.adapter()
		ctx := context.Background()
		_, err := a.companyName(ctx, sales, "ACME-TH")
		require.NoError(t, err)
		for range 3 {
			_, err = a.companyName(ctx, sales, "NOPE")
			require.ErrorIs(t, err, erp.ErrValidation)
		}
		assert.Len(t, f.callsTo("GET /api/resource/Company"), 2, "the first miss reloads, the next ones do not")

		time.Sleep(companyMissTTL + time.Second)
		_, err = a.companyName(ctx, sales, "NOPE")
		require.ErrorIs(t, err, erp.ErrValidation)
		assert.Len(t, f.callsTo("GET /api/resource/Company"), 3, "a miss may reload again after the TTL")
	})
}

func TestWaitingForCompanyLoadRespectsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeERP(t)
		companyRoute(t, f, 8*time.Second, 200, nil)
		a := f.adapter()
		loaded := make(chan error, 1)
		go func() {
			_, err := a.companyName(context.Background(), principalFor("loader"), "ACME-TH")
			loaded <- err
		}()
		synctest.Wait() // the loader is inside the slow GET

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		start := time.Now()
		_, err := a.companyName(ctx, principalFor("waiter"), "ACME-TH")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, time.Second, time.Since(start), "returns when its own context ends")

		require.NoError(t, <-loaded)
		assert.Len(t, f.callsTo("GET /api/resource/Company"), 1)
	})
}

func TestFailedCompanyLoadIsSharedByWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeERP(t)
		companyRoute(t, f, time.Second, 503, nil)
		a := f.adapter()
		var wg sync.WaitGroup
		for i := range 3 {
			wg.Go(func() {
				_, err := a.companyName(context.Background(), principalFor(fmt.Sprintf("s%d", i)), "ACME-TH")
				assert.ErrorIs(t, err, erp.ErrUnavailable)
			})
		}
		wg.Wait()
		assert.Len(t, f.callsTo("GET /api/resource/Company"), 3, "one load (a GET and its two retries), not one per caller")
	})
}

func TestCancelledLoaderDoesNotFailWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newFakeERP(t)
		companyRoute(t, f, 2*time.Second, 200, nil)
		a := f.adapter()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		loaderDone := make(chan error, 1)
		go func() {
			_, err := a.companyName(ctx, principalFor("loader"), "ACME-TH")
			loaderDone <- err
		}()
		synctest.Wait()
		name, err := a.companyName(context.Background(), principalFor("waiter"), "ACME-TH")
		require.NoError(t, err, "the waiter loads for itself when the loader gave up")
		assert.Equal(t, companyName, name)
		require.ErrorIs(t, <-loaderDone, context.DeadlineExceeded)
	})
}
