package erpfake_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/erpfake"
	"github.com/atipongsena/erp-town/go/internal/erp/porttest"
)

func TestFakeConformance(t *testing.T) { porttest.Run(t, erpfake.DefaultEnv()) }

func TestFakeSeedNumbers(t *testing.T) {
	env := erpfake.DefaultEnv()
	ctx := context.Background()
	ref, err := env.Port.CreateSalesOrder(ctx, env.Sales, erp.SalesOrderInput{
		Company: env.Company, Customer: env.Customer,
		TransactionDate: env.Today, DeliveryDate: env.Today.AddDays(7),
		Lines: []erp.LineInput{{ItemCode: env.Item, Qty: erp.MustDecimal("3")}},
	})
	require.NoError(t, err)
	so, err := env.Port.GetSalesOrder(ctx, env.Sales, ref.Name)
	require.NoError(t, err)
	assert.Equal(t, "36", so.NetTotal.String())
	assert.Equal(t, "2.52", so.TaxTotal.String())
	assert.Equal(t, "38.52", so.GrandTotal.String())
	assert.Equal(t, "Draft", so.WorkflowState)
}

func TestFakeRejectsBadCredentials(t *testing.T) {
	env := erpfake.DefaultEnv()
	ctx := context.Background()
	ghost := erp.Principal{Kind: erp.PrincipalUser, ID: "ghost", Credential: erp.SessionCredential{SessionID: "nope"}}
	_, err := env.Port.GetSalesOrder(ctx, ghost, "SO-1")
	assert.ErrorIs(t, err, erp.ErrUnauthenticated)
	_, err = env.Port.GetSalesOrder(ctx, erp.Principal{ID: "x"}, "SO-1")
	assert.ErrorIs(t, err, erp.ErrUnauthenticated)
}

func TestFakeSubmitAfterRejectIsValidation(t *testing.T) {
	env := erpfake.DefaultEnv()
	ctx := context.Background()
	ref, err := env.Port.CreateSalesOrder(ctx, env.Sales, erp.SalesOrderInput{
		Company: env.Company, Customer: env.LimitedCustomer,
		TransactionDate: env.Today, DeliveryDate: env.Today.AddDays(7),
		Lines: []erp.LineInput{{ItemCode: env.BigItem, Qty: env.BigQty}},
	})
	require.NoError(t, err)
	res, err := env.Port.Submit(ctx, env.Sales, ref)
	require.NoError(t, err)
	assert.False(t, res.Submitted)
	// The sales user has no transition left while the order awaits approval.
	_, err = env.Port.Submit(ctx, env.Sales, ref)
	var ve *erp.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Contains(t, ve.Fields, "workflow")
	_, err = env.Port.Apply(ctx, env.Approver, ref, "ปฏิเสธ")
	require.NoError(t, err)
	so, err := env.Port.GetSalesOrder(ctx, env.Sales, ref.Name)
	require.NoError(t, err)
	assert.Equal(t, erp.DocDraft, so.Status)
	assert.Equal(t, "ปฏิเสธ", so.WorkflowState)
}
