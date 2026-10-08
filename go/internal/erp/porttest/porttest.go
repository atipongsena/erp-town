package porttest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// Run executes the conformance suite. Each case is an independent subtest that
// creates its own documents.
func Run(t *testing.T, env Env) {
	cases := []struct {
		name string
		fn   func(t *testing.T, env Env)
	}{
		{"CreateAndGet", createAndGet},
		{"SubmitWithinCredit", submitWithinCredit},
		{"SubmitOverCreditNeedsApproval", submitOverCreditNeedsApproval},
		{"FullSalesFlow", fullSalesFlow},
		{"ForbiddenForStockUser", forbiddenForStockUser},
		{"NotFound", notFound},
		{"UnknownCustomerIsValidation", unknownCustomerIsValidation},
		{"CancelSubmittedOrder", cancelSubmittedOrder},
		{"WhoamiReturnsRolesAndCompanies", whoami},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.fn(t, env) })
	}
}

func orderInput(env Env, customer, item string, qty erp.Decimal) erp.SalesOrderInput {
	return erp.SalesOrderInput{
		Company:         env.Company,
		Customer:        customer,
		TransactionDate: env.Today,
		DeliveryDate:    env.Today.AddDays(7),
		Lines:           []erp.LineInput{{ItemCode: item, Qty: qty}},
	}
}

func newOrder(t *testing.T, env Env, customer, item string, qty erp.Decimal) erp.DocRef {
	t.Helper()
	ref, err := env.Port.CreateSalesOrder(context.Background(), env.Sales, orderInput(env, customer, item, qty))
	require.NoError(t, err)
	require.Equal(t, erp.DocSalesOrder, ref.DocType)
	require.NotEmpty(t, ref.Name)
	return ref
}

func submit(t *testing.T, env Env, as erp.Principal, ref erp.DocRef) {
	t.Helper()
	res, err := env.Port.Submit(context.Background(), as, ref)
	require.NoError(t, err)
	require.True(t, res.Submitted, "expected submit to complete, workflow state %q", res.WorkflowState)
}

func createAndGet(t *testing.T, env Env) {
	ctx := context.Background()
	three := erp.MustDecimal("3")
	ref := newOrder(t, env, env.Customer, env.Item, three)

	so, err := env.Port.GetSalesOrder(ctx, env.Sales, ref.Name)
	require.NoError(t, err)
	assert.Equal(t, ref, so.Ref)
	assert.Equal(t, env.Company, so.Company)
	assert.Equal(t, env.Customer, so.Customer)
	assert.Equal(t, erp.DocDraft, so.Status)
	require.Len(t, so.Lines, 1)
	line := so.Lines[0]
	assert.Equal(t, env.Item, line.ItemCode)
	assert.Zero(t, line.Qty.Cmp(three))
	assert.Equal(t, 1, line.Rate.Cmp(erp.Decimal{}), "line rate comes from the price list")
	assert.Zero(t, line.Amount.Cmp(line.Rate.Mul(three)))
	assert.Zero(t, so.NetTotal.Cmp(line.Amount))

	assert.Zero(t, so.GrandTotal.Cmp(so.NetTotal.Add(so.TaxTotal)), "grand total = net + tax")
	// VAT 7 % exclusive, within one satang of net x 0.07.
	diff := so.TaxTotal.Sub(so.NetTotal.Mul(erp.MustDecimal("0.07")))
	if diff.Cmp(erp.Decimal{}) < 0 {
		diff = erp.Decimal{}.Sub(diff)
	}
	assert.LessOrEqual(t, diff.Cmp(erp.MustDecimal("0.01")), 0, "tax %s vs net %s", so.TaxTotal, so.NetTotal)
}

func submitWithinCredit(t *testing.T, env Env) {
	ctx := context.Background()
	ref := newOrder(t, env, env.Customer, env.Item, erp.MustDecimal("1"))
	res, err := env.Port.Submit(ctx, env.Sales, ref)
	require.NoError(t, err)
	assert.True(t, res.Submitted)

	so, err := env.Port.GetSalesOrder(ctx, env.Sales, ref.Name)
	require.NoError(t, err)
	assert.Equal(t, erp.DocSubmitted, so.Status)
}

func submitOverCreditNeedsApproval(t *testing.T, env Env) {
	ctx := context.Background()
	ref := newOrder(t, env, env.LimitedCustomer, env.BigItem, env.BigQty)

	res, err := env.Port.Submit(ctx, env.Sales, ref)
	require.NoError(t, err)
	assert.False(t, res.Submitted)
	assert.Equal(t, env.PendingState, res.WorkflowState)

	so, err := env.Port.GetSalesOrder(ctx, env.Sales, ref.Name)
	require.NoError(t, err)
	assert.Equal(t, erp.DocDraft, so.Status)
	assert.Equal(t, env.PendingState, so.WorkflowState)

	salesActions, err := env.Port.Actions(ctx, env.Sales, ref)
	require.NoError(t, err)
	assert.NotContains(t, salesActions, env.ApproveAction)
	_, err = env.Port.Apply(ctx, env.Sales, ref, env.ApproveAction)
	require.Error(t, err)

	actions, err := env.Port.Actions(ctx, env.Approver, ref)
	require.NoError(t, err)
	require.Contains(t, actions, env.ApproveAction)

	res, err = env.Port.Apply(ctx, env.Approver, ref, env.ApproveAction)
	require.NoError(t, err)
	assert.True(t, res.Submitted)

	so, err = env.Port.GetSalesOrder(ctx, env.Sales, ref.Name)
	require.NoError(t, err)
	assert.Equal(t, erp.DocSubmitted, so.Status)
}

func fullSalesFlow(t *testing.T, env Env) {
	ctx := context.Background()
	so := newOrder(t, env, env.Customer, env.Item, erp.MustDecimal("2"))
	submit(t, env, env.Sales, so)

	dnRef, err := env.Port.CreateDeliveryNote(ctx, env.Stock, so, nil)
	require.NoError(t, err)
	require.Equal(t, erp.DocDeliveryNote, dnRef.DocType)
	submit(t, env, env.Stock, dnRef)
	dn, err := env.Port.GetDeliveryNote(ctx, env.Stock, dnRef.Name)
	require.NoError(t, err)
	assert.Equal(t, erp.DocSubmitted, dn.Status)
	assert.Contains(t, dn.AgainstSalesOrders, so.Name)

	siRef, err := env.Port.CreateSalesInvoice(ctx, env.Accounts, dnRef, nil)
	require.NoError(t, err)
	require.Equal(t, erp.DocSalesInvoice, siRef.DocType)
	submit(t, env, env.Accounts, siRef)
	si, err := env.Port.GetSalesInvoice(ctx, env.Accounts, siRef.Name)
	require.NoError(t, err)
	assert.Equal(t, erp.DocSubmitted, si.Status)
	assert.False(t, si.IsReturn)
	assert.Equal(t, 1, si.Outstanding.Cmp(erp.Decimal{}), "unpaid invoice has an outstanding amount")
	assert.Zero(t, si.Outstanding.Cmp(si.GrandTotal))

	peRef, err := env.Port.CreatePayment(ctx, env.Accounts, erp.PaymentInput{Invoice: siRef.Name, ModeOfPayment: env.ModeOfPayment})
	require.NoError(t, err)
	require.Equal(t, erp.DocPaymentEntry, peRef.DocType)
	submit(t, env, env.Accounts, peRef)

	si, err = env.Port.GetSalesInvoice(ctx, env.Accounts, siRef.Name)
	require.NoError(t, err)
	assert.Zero(t, si.Outstanding.Cmp(erp.Decimal{}), "outstanding %s", si.Outstanding)

	pe, err := env.Port.GetPaymentEntry(ctx, env.Accounts, peRef.Name)
	require.NoError(t, err)
	assert.Equal(t, erp.DocSubmitted, pe.Status)
	assert.Equal(t, env.ModeOfPayment, pe.ModeOfPayment)
	assert.Contains(t, pe.AgainstInvoices, siRef.Name)
	assert.Zero(t, pe.Paid.Cmp(si.GrandTotal))
}

func forbiddenForStockUser(t *testing.T, env Env) {
	_, err := env.Port.CreateSalesOrder(context.Background(), env.Stock, orderInput(env, env.Customer, env.Item, erp.MustDecimal("1")))
	assert.ErrorIs(t, err, erp.ErrForbidden)
}

func notFound(t *testing.T, env Env) {
	_, err := env.Port.GetSalesOrder(context.Background(), env.Sales, "SO-NOPE")
	assert.ErrorIs(t, err, erp.ErrNotFound)
}

func unknownCustomerIsValidation(t *testing.T, env Env) {
	_, err := env.Port.CreateSalesOrder(context.Background(), env.Sales, orderInput(env, "CUST-NOPE", env.Item, erp.MustDecimal("1")))
	assert.ErrorIs(t, err, erp.ErrValidation)
}

func cancelSubmittedOrder(t *testing.T, env Env) {
	ctx := context.Background()
	ref := newOrder(t, env, env.Customer, env.Item, erp.MustDecimal("1"))
	submit(t, env, env.Sales, ref)

	require.NoError(t, env.Port.Cancel(ctx, env.Sales, ref))
	so, err := env.Port.GetSalesOrder(ctx, env.Sales, ref.Name)
	require.NoError(t, err)
	assert.Equal(t, erp.DocCancelled, so.Status)
}

func whoami(t *testing.T, env Env) {
	ctx := context.Background()
	id, err := env.Port.Whoami(ctx, env.Sales.Credential)
	require.NoError(t, err)
	assert.Equal(t, env.Sales.ID, id.User)
	assert.Contains(t, id.Roles, "Sales User")
	assert.Contains(t, id.Companies, env.Company)

	_, err = env.Port.Whoami(ctx, erp.SessionCredential{SessionID: "no-such-session"})
	assert.ErrorIs(t, err, erp.ErrUnauthenticated)
}
