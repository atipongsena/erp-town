package agents

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/erpfake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type authorityPort struct {
	erp.Port
	mu              sync.Mutex
	enabled         bool
	delegateCompany string
	authorityCalls  int
	denyReference   bool
}

func (p *authorityPort) CheckDraftAccess(ctx context.Context, cred erp.Credential, in erp.DraftAccessInput) error {
	if p.denyReference && cred == (erp.SessionCredential{SessionID: "human"}) {
		return erp.ErrForbidden
	}
	return p.Port.CheckDraftAccess(ctx, cred, in)
}

func TestPolicyChecksDelegatorReferencePermissionsBeforeIntent(t *testing.T) {
	x, p, l := policyFixture(t)
	p.denyReference = true
	out, err := x.Execute(t.Context(), draftCall(draftArgs))
	require.NoError(t, err)
	assert.True(t, out.NeedsHuman)
	assert.Empty(t, l.calls)
}

func (p *authorityPort) AgentAccess(ctx context.Context, c erp.Credential) (erp.AgentAccess, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.authorityCalls++
	id, err := p.Whoami(ctx, c)
	if err != nil {
		return erp.AgentAccess{}, err
	}
	key := ""
	if id.User == "agent@test" && p.enabled {
		key = "sales_helper"
	}
	if id.User == "human@test" {
		id.Companies = []string{p.delegateCompany}
	}
	perms := map[erp.DocType]erp.DocumentPermission{}
	for _, dt := range []erp.DocType{erp.DocSalesOrder, erp.DocDeliveryNote, erp.DocSalesInvoice, erp.DocPaymentEntry} {
		perms[dt] = erp.DocumentPermission{Read: true, Create: true}
	}
	return erp.AgentAccess{Identity: id, AgentKey: key, Permissions: perms}, nil
}

type memoryLedger struct {
	mu       sync.Mutex
	calls    map[string]Intent
	results  map[string]SafeResult
	attempts int
	fail     bool
}

func (l *memoryLedger) Attempt(context.Context, string, string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attempts++
	return l.attempts <= 30, nil
}

func (l *memoryLedger) Claim(_ context.Context, in Intent) (Claim, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail {
		return Claim{}, errPolicyStorage
	}
	if old, ok := l.calls[in.CallID]; ok {
		if old.Hash != in.Hash || old.Tool != in.Tool {
			return Claim{State: ClaimConflict}, nil
		}
		if r, ok := l.results[in.CallID]; ok {
			return Claim{State: ClaimCompleted, Result: r}, nil
		}
		return Claim{State: ClaimUncertain}, nil
	}
	l.calls[in.CallID] = in
	return Claim{State: ClaimNew}, nil
}

func (l *memoryLedger) Finish(_ context.Context, in Intent, r SafeResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail {
		return errPolicyStorage
	}
	l.results[in.CallID] = r
	return nil
}

func policyFixture(t *testing.T) (*PolicyExecutor, *authorityPort, *memoryLedger) {
	t.Helper()
	users := map[string]erpfake.FakeUser{"agent": {User: "agent@test", Roles: []string{"Sales User"}, Companies: []string{"ACME"}}, "human": {User: "human@test", Roles: []string{"Sales User"}, Companies: []string{"ACME"}}}
	p := &authorityPort{Port: erpfake.New(erpfake.Config{Company: "ACME", Users: users, CreditLimits: map[string]erp.Decimal{"CUST": erp.MustDecimal("10000")}, Prices: map[string]erp.Decimal{"ITEM": erp.MustDecimal("10")}}), enabled: true, delegateCompany: "ACME"}
	l := &memoryLedger{calls: map[string]Intent{}, results: map[string]SafeResult{}}
	x, err := NewPolicyExecutor(p, l, Binding{RunID: "RUN-1", AgentKey: "sales_helper", Company: "ACME", AgentCredential: erp.SessionCredential{SessionID: "agent"}, DelegatorCredential: erp.SessionCredential{SessionID: "human"}})
	require.NoError(t, err)
	return x, p, l
}

func draftCall(args string) Call {
	return Call{ID: "draft-1", Tool: "create_sales_order_draft", Arguments: json.RawMessage(args)}
}

const draftArgs = `{"customer":"CUST","transaction_date":"2026-10-11","delivery_date":"2026-10-12","lines":[{"item_code":"ITEM","qty":"1"}]}`

type sourceChangedPort struct {
	erp.Port
	active bool
	mode   string
	writes int
}

func (p *sourceChangedPort) GetSalesOrder(ctx context.Context, as erp.Principal, name string) (erp.SalesOrder, error) {
	doc, err := p.Port.GetSalesOrder(ctx, as, name)
	if !p.active || err != nil {
		return doc, err
	}
	switch p.mode {
	case "permission":
		return erp.SalesOrder{}, erp.ErrForbidden
	case "quantity":
		doc.Lines = append([]erp.Line(nil), doc.Lines...)
		doc.Lines[0].Qty = erp.MustDecimal("101")
	case "cancelled":
		doc.Status = erp.DocCancelled
	case "customer":
		doc.Customer = "HIDDEN"
	}
	return doc, nil
}

func (p *sourceChangedPort) CreateDeliveryNote(ctx context.Context, as erp.Principal, ref erp.DocRef, date *erp.Date) (erp.DocRef, error) {
	p.writes++
	return p.Port.CreateDeliveryNote(ctx, as, ref, date)
}

type afterClaimLedger struct {
	PolicyLedger
	change func()
}

func (l afterClaimLedger) Claim(ctx context.Context, in Intent) (Claim, error) {
	result, err := l.PolicyLedger.Claim(ctx, in)
	if result.State == ClaimNew && err == nil {
		l.change()
	}
	return result, err
}

func TestPolicyRechecksSourceFactsAndReadVisibilityAfterClaim(t *testing.T) {
	for _, mode := range []string{"permission", "quantity", "cancelled", "customer"} {
		t.Run(mode, func(t *testing.T) {
			x, port, ledger := policyFixture(t)
			users := map[string]erpfake.FakeUser{
				"agent": {User: "agent@test", Roles: []string{"Sales User", "Stock User"}, Companies: []string{"ACME"}},
				"human": {User: "human@test", Roles: []string{"Sales User", "Stock User"}, Companies: []string{"ACME"}},
			}
			port.Port = erpfake.New(erpfake.Config{Company: "ACME", Users: users, CreditLimits: map[string]erp.Decimal{"CUST": erp.MustDecimal("10000")}, Prices: map[string]erp.Decimal{"ITEM": erp.MustDecimal("10")}})
			out, err := x.Execute(t.Context(), draftCall(draftArgs))
			require.NoError(t, err)
			require.False(t, out.NeedsHuman)
			var safe SafeResult
			require.NoError(t, json.Unmarshal(out.Output, &safe))
			_, err = port.Submit(t.Context(), erp.Principal{Credential: erp.SessionCredential{SessionID: "human"}}, safe.Ref)
			require.NoError(t, err)
			changed := &sourceChangedPort{Port: port, mode: mode}
			x.port = changed
			x.ledger = afterClaimLedger{PolicyLedger: ledger, change: func() { changed.active = true }}
			tool := "create_delivery_note_draft"
			if mode == "permission" {
				tool = "read_sales_order"
			}
			raw, err := json.Marshal(map[string]string{"name": safe.Ref.Name})
			require.NoError(t, err)
			out, err = x.Execute(t.Context(), Call{ID: "source-check", Tool: tool, Arguments: raw})
			require.NoError(t, err)
			require.Contains(t, ledger.calls, "source-check", "the regression must reach the durable claim")
			require.True(t, out.NeedsHuman)
			require.Zero(t, changed.writes)
		})
	}
}

func TestPolicyAllEightToolsRequireHumanConfirmationToSettle(t *testing.T) {
	x, port, ledger := policyFixture(t)
	users := map[string]erpfake.FakeUser{}
	for key, user := range map[string]string{"agent": "agent@test", "human": "human@test"} {
		users[key] = erpfake.FakeUser{User: user, Roles: []string{"Sales User", "Stock User", "Accounts User"}, Companies: []string{"ACME"}}
	}
	port.Port = erpfake.New(erpfake.Config{Company: "ACME", Users: users, CreditLimits: map[string]erp.Decimal{"CUST": erp.MustDecimal("10000")}, Prices: map[string]erp.Decimal{"ITEM": erp.MustDecimal("10")}})
	human := erp.Principal{Credential: erp.SessionCredential{SessionID: "human"}}
	tool := func(id, name string, args []byte) SafeResult {
		t.Helper()
		result, err := x.Execute(t.Context(), Call{ID: id, Tool: name, Arguments: args})
		require.NoError(t, err)
		require.False(t, result.NeedsHuman, string(result.Output))
		var safe SafeResult
		require.NoError(t, json.Unmarshal(result.Output, &safe))
		return safe
	}
	first := tool("draft-so", "create_sales_order_draft", []byte(draftArgs))
	ref := first.Ref
	var invoice erp.DocRef
	for _, step := range []struct{ read, next string }{
		{"read_sales_order", "create_delivery_note_draft"},
		{"read_delivery_note", "create_sales_invoice_draft"},
		{"read_sales_invoice", "create_payment_draft"},
		{"read_payment_entry", ""},
	} {
		args, err := json.Marshal(map[string]string{"name": ref.Name})
		require.NoError(t, err)
		before := tool(step.read, step.read, args)
		require.Equal(t, erp.DocDraft, before.Status)
		if ref.DocType == erp.DocPaymentEntry {
			unpaid, err := port.GetSalesInvoice(t.Context(), human, invoice.Name)
			require.NoError(t, err)
			require.Positive(t, unpaid.Outstanding.Cmp(erp.Decimal{}))
		}
		submitted, err := port.Submit(t.Context(), human, ref)
		require.NoError(t, err)
		require.True(t, submitted.Submitted)
		if ref.DocType == erp.DocSalesInvoice {
			invoice = ref
		}
		if step.next != "" {
			if step.next == "create_payment_draft" {
				args, err = json.Marshal(map[string]string{"name": ref.Name, "mode_of_payment": "Cash"})
				require.NoError(t, err)
			}
			next := tool(step.next, step.next, args)
			require.Equal(t, erp.DocDraft, next.Status)
			ref = next.Ref
		}
	}
	settled, err := port.GetSalesInvoice(t.Context(), human, invoice.Name)
	require.NoError(t, err)
	require.Zero(t, settled.Outstanding.Cmp(erp.Decimal{}))
	require.Len(t, ledger.calls, 8)
}

func TestPolicyBindsAgentAndDelegatorAndCreatesOnlyDraft(t *testing.T) {
	x, p, l := policyFixture(t)
	require.NoError(t, x.Validate(t.Context()))
	out, err := x.Execute(t.Context(), draftCall(draftArgs))
	require.NoError(t, err)
	require.False(t, out.NeedsHuman)
	var safe SafeResult
	require.NoError(t, json.Unmarshal(out.Output, &safe))
	assert.Equal(t, erp.DocDraft, safe.Status)
	so, err := p.GetSalesOrder(t.Context(), erp.Principal{Credential: erp.SessionCredential{SessionID: "agent"}}, safe.Ref.Name)
	require.NoError(t, err)
	assert.Equal(t, "ACME", so.Company)
	require.Equal(t, 6, p.authorityCalls)
	require.Len(t, l.calls, 1)
	require.Len(t, l.results, 1)
	assert.NotContains(t, string(out.Output), "agent@test")
	assert.NotContains(t, string(out.Output), "10")
}

func TestPolicyRevocationStopsFurtherEffects(t *testing.T) {
	for _, revocation := range []string{"disabled", "delegate_company"} {
		t.Run(revocation, func(t *testing.T) {
			x, p, l := policyFixture(t)
			require.NoError(t, x.Validate(t.Context()))
			if revocation == "disabled" {
				p.enabled = false
			} else {
				p.delegateCompany = "OTHER"
			}
			out, err := x.Execute(t.Context(), draftCall(draftArgs))
			require.NoError(t, err)
			assert.True(t, out.NeedsHuman)
			assert.Empty(t, l.calls)
		})
	}
}

func TestPolicyRejectsAuthorityClaimsAndNonExactQuantities(t *testing.T) {
	cases := []string{
		`{"company":"OTHER",` + draftArgs[1:], `{"on_behalf_of":"victim",` + draftArgs[1:],
		`{"customer":"CUST","transaction_date":"2026-10-11","delivery_date":"2026-10-12","lines":[{"item_code":"ITEM","qty":1}]}`,
		`{"customer":"CUST","transaction_date":"2026-10-11","delivery_date":"2026-10-12","lines":[{"item_code":"ITEM","qty":"100.00000000000000000001"}]}`,
		`{"customer":"CUST","transaction_date":"2026-10-11","delivery_date":"2026-10-12","lines":[{"item_code":"ITEM","qty":"0"}]}`,
		`{"customer":"CUST","transaction_date":"2026-10-11","delivery_date":"2026-10-12","lines":[{"item_code":"ITEM","qty":"-1"}]}`,
		`{"customer":"CUST","transaction_date":"2026-10-11","delivery_date":"2026-10-12","lines":[{"item_code":"ITEM","qty":"1","rate":"0"}]}`,
		`{"customer":"CUST","customer":"OTHER","transaction_date":"2026-10-11","delivery_date":"2026-10-12","lines":[{"item_code":"ITEM","qty":"1"}]}`,
		draftArgs + `{}`, `null`,
	}
	for _, args := range cases {
		t.Run(args, func(t *testing.T) {
			x, _, l := policyFixture(t)
			out, err := x.Execute(t.Context(), draftCall(args))
			require.NoError(t, err)
			assert.True(t, out.NeedsHuman)
			assert.Empty(t, l.calls)
		})
	}
}

func TestPolicyDraftUnitLimitIsTotalAcrossLines(t *testing.T) {
	x, _, l := policyFixture(t)
	args := `{"customer":"CUST","transaction_date":"2026-10-11","delivery_date":"2026-10-12","lines":[{"item_code":"ITEM","qty":"60"},{"item_code":"ITEM","qty":"40.00000000000000000001"}]}`
	out, err := x.Execute(t.Context(), draftCall(args))
	require.NoError(t, err)
	assert.True(t, out.NeedsHuman)
	assert.Empty(t, l.calls)
}

func TestPolicyNeverExposesLifecycleTools(t *testing.T) {
	x, _, l := policyFixture(t)
	defs := x.Tools()
	require.Len(t, defs, 8)
	for _, tool := range []string{"submit", "cancel", "apply_workflow", "settle_payment"} {
		out, err := x.Execute(t.Context(), Call{ID: "bad", Tool: tool, Arguments: json.RawMessage(`{}`)})
		require.NoError(t, err)
		assert.True(t, out.NeedsHuman)
	}
	assert.Empty(t, l.calls)
	defs[0].InputSchema[0] = '!'
	assert.True(t, json.Valid(x.Tools()[0].InputSchema))
}

func TestPolicyUnresolvedIntentAndConflictCannotRedispatch(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "unresolved", true: "conflict"}[conflict], func(t *testing.T) {
			x, _, l := policyFixture(t)
			out, err := x.Execute(t.Context(), draftCall(draftArgs))
			require.NoError(t, err)
			require.False(t, out.NeedsHuman)
			delete(l.results, "draft-1")
			if conflict {
				in := l.calls["draft-1"]
				in.Hash = "different"
				l.calls["draft-1"] = in
			}
			out, err = x.Execute(t.Context(), draftCall(draftArgs))
			require.NoError(t, err)
			assert.True(t, out.NeedsHuman)
			assert.Empty(t, l.results)
		})
	}
}

func TestPolicyCanonicalReplayReturnsSameDraft(t *testing.T) {
	x, _, l := policyFixture(t)
	first, err := x.Execute(t.Context(), draftCall(draftArgs))
	require.NoError(t, err)
	reordered := `{"lines":[{"qty":"1","item_code":"ITEM"}],"delivery_date":"2026-10-12","transaction_date":"2026-10-11","customer":"CUST"}`
	next, err := x.Execute(t.Context(), draftCall(reordered))
	require.NoError(t, err)
	assert.JSONEq(t, string(first.Output), string(next.Output))
	assert.Len(t, l.calls, 1)
}

func TestPolicyRateLimitAndStorageFailureFailClosed(t *testing.T) {
	x, _, l := policyFixture(t)
	l.attempts = 30
	out, err := x.Execute(t.Context(), draftCall(draftArgs))
	require.NoError(t, err)
	assert.True(t, out.NeedsHuman)
	assert.Empty(t, l.calls)
	x, _, l = policyFixture(t)
	l.fail = true
	out, err = x.Execute(t.Context(), draftCall(draftArgs))
	require.NoError(t, err)
	assert.True(t, out.NeedsHuman)
	assert.Empty(t, l.calls)
}
