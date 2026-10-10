package erpfake

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/porttest"
)

func TestProjectionReadsOnlyCompanyMetadata(t *testing.T) {
	f := New(Config{Company: "ACME-TH", Today: erp.MustDate("2026-10-10"), Users: map[string]FakeUser{"world": {User: "world@example.test", Roles: []string{"Town Projection Reader"}, Companies: []string{"ACME-TH"}}}})
	so := erp.DocRef{DocType: erp.DocSalesOrder, Name: "SO-1"}
	f.so[so.Name] = &salesOrder{erp.SalesOrder{Ref: so, Company: "ACME-TH", Customer: "private-customer", Status: erp.DocSubmitted, GrandTotal: erp.MustDecimal("123.45")}}
	si := erp.DocRef{DocType: erp.DocSalesInvoice, Name: "SI-1"}
	f.si[si.Name] = &salesInvoice{SalesInvoice: erp.SalesInvoice{Ref: si, Company: "ACME-TH", Status: erp.DocSubmitted, DueDate: erp.MustDate("2026-10-08"), Outstanding: erp.MustDecimal("123.45")}}
	as := erp.Principal{Kind: erp.PrincipalSystem, ID: "system.town", Credential: erp.SessionCredential{SessionID: "world"}}
	porttest.RunProjection(t, f, as, "ACME-TH")
	out, err := f.ReadProjection(t.Context(), as, "ACME-TH")
	require.NoError(t, err)
	require.Equal(t, "ACME-TH", out.Company)
	require.Len(t, out.Facts, 2)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	for _, secret := range []string{"123.45", "private-customer", "GrandTotal", "Outstanding"} {
		assert.NotContains(t, string(raw), secret)
	}
	f.si[si.Name].Outstanding = erp.Decimal{}
	paid, err := f.ReadProjection(t.Context(), as, "ACME-TH")
	require.NoError(t, err)
	require.Len(t, paid.Facts, 1)
	require.Len(t, paid.Sequences, 2, "resolved subjects still retain a reconciliation fence")
	as.ID = "agent.1"
	_, err = f.ReadProjection(t.Context(), as, "ACME-TH")
	require.ErrorIs(t, err, erp.ErrForbidden)
	as.ID = "system.town"
	_, err = f.ReadProjection(t.Context(), as, "OTHER")
	require.ErrorIs(t, err, erp.ErrForbidden)
}

func TestProjectionRequiresDedicatedCredentialRole(t *testing.T) {
	f := New(Config{Company: "ACME-TH", Users: map[string]FakeUser{"sales": {Roles: []string{RoleSalesUser}, Companies: []string{"ACME-TH"}}}})
	as := erp.Principal{Kind: erp.PrincipalSystem, ID: "system.town", Roles: []string{"Town Projection Reader"}, Companies: []string{"ACME-TH"}, Credential: erp.SessionCredential{SessionID: "sales"}}
	_, err := f.ReadProjection(t.Context(), as, "ACME-TH")
	require.ErrorIs(t, err, erp.ErrForbidden, "principal claims cannot grant the credential's role")
	as.Credential = nil
	_, err = f.ReadProjection(t.Context(), as, "ACME-TH")
	require.ErrorIs(t, err, erp.ErrUnauthenticated)
}
