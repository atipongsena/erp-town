//go:build integration

package erpnext

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/porttest"
)

// Run only through `pnpm go:test:integration`, which seeds test.localhost
// around the whole run and exports TOWN_TEST_ERP_URL / TOWN_TEST_ERP_SITE.

const defaultSeedPassword = "town-dev-123"

type erpSite struct{ url, site string }

func testSite(t *testing.T) erpSite {
	t.Helper()
	s := erpSite{url: os.Getenv("TOWN_TEST_ERP_URL"), site: os.Getenv("TOWN_TEST_ERP_SITE")}
	if s.url == "" || s.site == "" {
		t.Skip("TOWN_TEST_ERP_URL/TOWN_TEST_ERP_SITE not set: run pnpm go:test:integration")
	}
	return s
}

// login signs in with the seeded password and returns the session id.
func (s erpSite) login(t *testing.T, user string) string {
	t.Helper()
	pwd := os.Getenv("SEED_USER_PASSWORD")
	if pwd == "" {
		pwd = defaultSeedPassword
	}
	body, err := json.Marshal(map[string]string{"usr": user, "pwd": pwd})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url+"/api/method/login", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Frappe-Site-Name", s.site)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusOK, res.StatusCode, "login %s", user)
	for _, c := range res.Cookies() {
		if c.Name == "sid" && c.Value != "" && c.Value != "Guest" {
			return c.Value
		}
	}
	t.Fatalf("login %s: no sid cookie", user)
	return ""
}

// bangkokToday is the site's date (Asia/Bangkok): seeded prices are valid from the seed day.
func bangkokToday() erp.Date {
	now := time.Now().In(time.FixedZone("ICT", 7*60*60))
	return erp.Date{Year: now.Year(), Month: now.Month(), Day: now.Day()}
}

func TestERPNextConformance(t *testing.T) {
	s := testSite(t)
	a := NewAdapter(NewClient(ClientConfig{BaseURL: s.url, Site: s.site}))
	ctx := context.Background()

	principal := func(user string) erp.Principal {
		t.Helper()
		sid := s.login(t, user)
		id, err := a.Whoami(ctx, erp.SessionCredential{SessionID: sid})
		require.NoError(t, err)
		require.Equal(t, user, id.User)
		sc, ok := id.Credential.(erp.SessionCredential)
		require.True(t, ok)
		require.NotEmpty(t, sc.CSRFToken, "whoami hands out the session's CSRF token")

		// the gateway knows only the cookie: a second whoami without the token still works
		again, err := a.Whoami(ctx, erp.SessionCredential{SessionID: sid})
		require.NoError(t, err)
		require.Equal(t, sc, again.Credential, "the session keeps its CSRF token")
		return erp.Principal{
			Kind: erp.PrincipalUser, ID: id.User, Roles: id.Roles, Companies: id.Companies, Credential: id.Credential,
		}
	}

	env := porttest.Env{
		Port:            a,
		Company:         "ACME-TH",
		Customer:        "CUST-001",
		LimitedCustomer: "CUST-009",
		Item:            "ITEM-002",
		BigItem:         "ITEM-018",
		ItemRate:        erp.MustDecimal("12"),
		BigQty:          erp.MustDecimal("2"),
		ModeOfPayment:   "เงินสด",
		PendingState:    "รออนุมัติ",
		ApproveAction:   "อนุมัติ",
		Sales:           principal("sales@acme-th.test"),
		Approver:        principal("approver@acme-th.test"),
		Stock:           principal("stock@acme-th.test"),
		Accounts:        principal("accounts@acme-th.test"),
		Today:           bangkokToday(),
	}
	porttest.Run(t, env)

	t.Run("ExplicitPostingDates", func(t *testing.T) {
		today := env.Today
		so, err := a.CreateSalesOrder(ctx, env.Sales, erp.SalesOrderInput{
			Company: env.Company, Customer: env.Customer, TransactionDate: today, DeliveryDate: today,
			Lines: []erp.LineInput{{ItemCode: env.Item, Qty: erp.MustDecimal("1")}},
		})
		require.NoError(t, err)
		_, err = a.Submit(ctx, env.Sales, so)
		require.NoError(t, err)

		dnRef, err := a.CreateDeliveryNote(ctx, env.Stock, so, &today)
		require.NoError(t, err)
		_, err = a.Submit(ctx, env.Stock, dnRef)
		require.NoError(t, err)
		siRef, err := a.CreateSalesInvoice(ctx, env.Accounts, dnRef, &today)
		require.NoError(t, err)
		_, err = a.Submit(ctx, env.Accounts, siRef)
		require.NoError(t, err)
		si, err := a.GetSalesInvoice(ctx, env.Accounts, siRef.Name)
		require.NoError(t, err)
		assert.Equal(t, today, si.PostingDate)
		assert.False(t, si.DueDate.Before(today))

		peRef, err := a.CreatePayment(ctx, env.Accounts, erp.PaymentInput{Invoice: siRef.Name, ModeOfPayment: env.ModeOfPayment, PostingDate: &today})
		require.NoError(t, err)
		_, err = a.Submit(ctx, env.Accounts, peRef)
		require.NoError(t, err)
		pe, err := a.GetPaymentEntry(ctx, env.Accounts, peRef.Name)
		require.NoError(t, err)
		assert.Equal(t, today, pe.PostingDate)
	})

	t.Run("CashIsReceivedIntoTheCashAccount", func(t *testing.T) {
		// the last payment of the flow above: its account comes from the mode of payment
		var pes []struct {
			PaidTo string `json:"paid_to"`
		}
		err := a.c.Get(ctx, env.Accounts, ResourcePath("Payment Entry", ""), map[string][]string{
			"fields":            {`["paid_to"]`},
			"filters":           {`{"mode_of_payment":"เงินสด","docstatus":1}`},
			"limit_page_length": {"0"},
		}, &pes)
		require.NoError(t, err)
		require.NotEmpty(t, pes)
		for _, pe := range pes {
			assert.Contains(t, pe.PaidTo, "เงินสด")
		}
	})
}
