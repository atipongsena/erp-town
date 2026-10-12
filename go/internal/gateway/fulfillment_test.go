package gateway_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/erpfake"
)

func TestGatewaySalesToCash(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	create := func(path, body, session string) string {
		t.Helper()
		w := h.do(http.MethodPost, path, body, withSession(session), withTownHeader())
		require.Equal(t, 201, w.Code, w.Body.String())
		return decode[struct{ Name string }](t, w).Name
	}
	submit := func(kind, name, session string) {
		t.Helper()
		w := h.do(http.MethodPost, "/town/api/documents/"+kind+"/"+name+"/submit", "", withSession(session), withTownHeader())
		require.Equal(t, 200, w.Code, w.Body.String())
		require.True(t, decode[struct{ Submitted bool }](t, w).Submitted)
	}
	so := create("/town/api/sales-orders", orderBody, salesSession)
	submit("sales-order", so, salesSession)
	dnBody := fmt.Sprintf(`{"company":"ACME-TH","sales_order":%q,"posting_date":"2026-10-09"}`, so)
	denied := h.do(http.MethodPost, "/town/api/delivery-notes", dnBody, withSession(salesSession), withTownHeader())
	require.Equal(t, 403, denied.Code)
	dn := create("/town/api/delivery-notes", dnBody, "sess-stock")
	w := h.do(http.MethodGet, "/town/api/delivery-notes/"+dn, "", withSession("sess-stock"))
	require.Equal(t, 200, w.Code)
	delivery := decode[struct {
		Company string   `json:"company"`
		Posting string   `json:"posting_date"`
		Against []string `json:"against_sales_orders"`
	}](t, w)
	assert.Equal(t, "ACME-TH", delivery.Company)
	assert.Equal(t, "2026-10-09", delivery.Posting)
	assert.Equal(t, []string{so}, delivery.Against)
	submit("delivery-note", dn, "sess-stock")
	si := create("/town/api/sales-invoices", fmt.Sprintf(`{"company":"ACME-TH","delivery_note":%q}`, dn), "sess-accounts")
	submit("sales-invoice", si, "sess-accounts")
	w = h.do(http.MethodGet, "/town/api/sales-invoices/"+si, "", withSession("sess-accounts"))
	require.Equal(t, 200, w.Code)
	invoice := decode[struct {
		Against     []string `json:"against_delivery_notes"`
		Rounded     string   `json:"rounded_total"`
		Outstanding string   `json:"outstanding"`
	}](t, w)
	require.NotEmpty(t, invoice.Rounded)
	assert.Equal(t, []string{dn}, invoice.Against)
	assert.Equal(t, invoice.Rounded, invoice.Outstanding)
	pe := create("/town/api/payment-entries", fmt.Sprintf(`{"company":"ACME-TH","sales_invoice":%q,"mode_of_payment":"เงินสด"}`, si), "sess-accounts")
	w = h.do(http.MethodGet, "/town/api/payment-entries/"+pe, "", withSession("sess-accounts"))
	require.Equal(t, 200, w.Code)
	payment := decode[struct {
		Paid    string   `json:"paid"`
		Status  string   `json:"status"`
		Against []string `json:"against_invoices"`
	}](t, w)
	assert.Equal(t, invoice.Outstanding, payment.Paid)
	assert.Equal(t, "Draft", payment.Status)
	assert.Equal(t, []string{si}, payment.Against)
	submit("payment-entry", pe, "sess-accounts")
	w = h.do(http.MethodGet, "/town/api/sales-invoices/"+si, "", withSession("sess-accounts"))
	assert.Equal(t, "0", decode[struct{ Outstanding string }](t, w).Outstanding)
}

type fulfillmentSourcePort struct {
	erp.Port
	company string
	status  erp.DocStatus
	creates int
}

func (p *fulfillmentSourcePort) GetSalesOrder(context.Context, erp.Principal, string) (erp.SalesOrder, error) {
	return erp.SalesOrder{Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: "SO-1"}, Company: p.company, Status: p.status}, nil
}

func (p *fulfillmentSourcePort) GetDeliveryNote(context.Context, erp.Principal, string) (erp.DeliveryNote, error) {
	return erp.DeliveryNote{Ref: erp.DocRef{DocType: erp.DocDeliveryNote, Name: "DN-1"}, Company: p.company, Status: p.status}, nil
}

func (p *fulfillmentSourcePort) GetSalesInvoice(context.Context, erp.Principal, string) (erp.SalesInvoice, error) {
	return erp.SalesInvoice{Ref: erp.DocRef{DocType: erp.DocSalesInvoice, Name: "SI-1"}, Company: p.company, Status: p.status}, nil
}

func (p *fulfillmentSourcePort) CreateDeliveryNote(context.Context, erp.Principal, erp.DocRef, *erp.Date) (erp.DocRef, error) {
	p.creates++
	return erp.DocRef{}, nil
}

func (p *fulfillmentSourcePort) CreateSalesInvoice(context.Context, erp.Principal, erp.DocRef, *erp.Date) (erp.DocRef, error) {
	p.creates++
	return erp.DocRef{}, nil
}

func (p *fulfillmentSourcePort) CreatePayment(context.Context, erp.Principal, erp.PaymentInput) (erp.DocRef, error) {
	p.creates++
	return erp.DocRef{}, nil
}

func TestFulfillmentRejectsForeignOrUnsubmittedSourceBeforeCreate(t *testing.T) {
	for _, endpoint := range []struct{ path, body string }{
		{"delivery-notes", `{"company":"ACME-TH","sales_order":"SO-1"}`},
		{"sales-invoices", `{"company":"ACME-TH","delivery_note":"DN-1"}`},
		{"payment-entries", `{"company":"ACME-TH","sales_invoice":"SI-1","mode_of_payment":"เงินสด"}`},
	} {
		for _, source := range []struct {
			company string
			status  erp.DocStatus
			want    int
		}{
			{"OTHER", erp.DocSubmitted, 403}, {"ACME-TH", erp.DocDraft, 422}, {"ACME-TH", erp.DocCancelled, 422},
		} {
			t.Run(endpoint.path+"/"+source.company+"/"+string(source.status), func(t *testing.T) {
				port := &fulfillmentSourcePort{Port: erpfake.DefaultEnv().Port, company: source.company, status: source.status}
				h := newHarness(t, port)
				w := h.do("POST", "/town/api/"+endpoint.path, endpoint.body, withSession("sess-accounts"), withTownHeader())
				require.Equal(t, source.want, w.Code, w.Body.String())
				require.Zero(t, port.creates, "no write may precede the source company/status checks")
			})
		}
	}
}

func TestFulfillmentGuardsAndDates(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	for _, tc := range []struct{ path, body string }{
		{"delivery-notes", `{"company":"ACME-TH","sales_order":"SO-1"}`},
		{"sales-invoices", `{"company":"ACME-TH","delivery_note":"DN-1"}`},
		{"payment-entries", `{"company":"ACME-TH","sales_invoice":"SI-1","mode_of_payment":"เงินสด"}`},
	} {
		w := h.do("POST", "/town/api/"+tc.path, tc.body, withSession(salesSession))
		assert.Equal(t, 403, w.Code)
		assert.Equal(t, "CSRF_HEADER_MISSING", decode[errBody](t, w).Code)
		w = h.do("POST", "/town/api/"+tc.path, tc.body, withTownHeader())
		assert.Equal(t, 401, w.Code)
	}
	for _, body := range []string{
		`{"company":"ACME-TH","sales_order":"SO-1","posting_date":"2026-02-30"}`,
		`{"company":"ACME-TH","sales_order":""}`,
	} {
		w := h.do("POST", "/town/api/delivery-notes", body, withSession("sess-stock"), withTownHeader())
		assert.Equal(t, 422, w.Code)
	}
	w := h.do("POST", "/town/api/delivery-notes", `{"company":"OTHER","sales_order":"SO-1"}`, withSession("sess-stock"), withTownHeader())
	assert.Equal(t, 403, w.Code)
}
