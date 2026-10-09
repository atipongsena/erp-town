package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// Error statuses each kind of operation documents.
var (
	readErrors  = []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError, http.StatusServiceUnavailable}
	writeErrors = []int{
		http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict,
		http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusServiceUnavailable,
	}
)

// --- identity ---

type meOutput struct {
	Body struct {
		User      string   `json:"user" doc:"ERP user id"`
		Roles     []string `json:"roles"`
		Companies []string `json:"companies" doc:"Company abbr codes the user may act for"`
	}
}

type tokenOutput struct {
	Body struct {
		Token     string    `json:"token" doc:"EdDSA (Ed25519) JWT for the WebSocket"`
		ExpiresAt time.Time `json:"expires_at"`
	}
}

func (g *gateway) registerIdentity(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-me",
		Method:      http.MethodGet,
		Path:        "/town/api/me",
		Summary:     "Current user",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized, http.StatusInternalServerError, http.StatusServiceUnavailable},
	}, func(ctx context.Context, _ *struct{}) (*meOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		out := &meOutput{}
		out.Body.User = p.ID
		out.Body.Roles = nonNil(p.Roles)
		out.Body.Companies = nonNil(p.Companies)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "issue-token",
		Method:      http.MethodPost,
		Path:        "/town/api/auth/token",
		Summary:     "Issue a short-lived JWT for the current user",
		Tags:        []string{"auth"},
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError, http.StatusServiceUnavailable},
	}, func(ctx context.Context, _ *struct{}) (*tokenOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		tok, exp, err := g.jwt.Issue(p)
		if err != nil {
			return nil, fail(ctx, err)
		}
		out := &tokenOutput{}
		out.Body.Token = tok
		out.Body.ExpiresAt = exp
		return out, nil
	})
}

// --- sales ---

// lineIn: qty and rate patterns are what erp.ParseDecimal accepts (decimal
// strings, never JSON numbers).
type lineIn struct {
	ItemCode string  `json:"item_code" minLength:"1"`
	Qty      string  `json:"qty" pattern:"^-?[0-9]+(\\.[0-9]+)?$" patternDescription:"decimal string" example:"2"`
	Rate     *string `json:"rate,omitempty" pattern:"^-?[0-9]+(\\.[0-9]+)?$" patternDescription:"decimal string" doc:"Omit to take the price list rate"`
}

type createSalesOrderInput struct {
	Body struct {
		Company         string   `json:"company" minLength:"1" doc:"Company abbr code" example:"ACME-TH"`
		Customer        string   `json:"customer" minLength:"1" example:"CUST-001"`
		TransactionDate string   `json:"transaction_date" format:"date"`
		DeliveryDate    string   `json:"delivery_date" format:"date"`
		Lines           []lineIn `json:"lines" minItems:"1"`
	}
}

type createdOutput struct {
	Body struct {
		Name string `json:"name"`
	}
}

type lineOut struct {
	ItemCode string `json:"item_code"`
	Qty      string `json:"qty"`
	Rate     string `json:"rate"`
	Amount   string `json:"amount"`
}

type salesOrderOutput struct {
	Body struct {
		Name            string    `json:"name"`
		Company         string    `json:"company"`
		Customer        string    `json:"customer"`
		TransactionDate *string   `json:"transaction_date" format:"date" doc:"null when unset"`
		DeliveryDate    *string   `json:"delivery_date" format:"date" doc:"null when unset"`
		Lines           []lineOut `json:"lines"`
		NetTotal        string    `json:"net_total"`
		TaxTotal        string    `json:"tax_total"`
		GrandTotal      string    `json:"grand_total"`
		RoundedTotal    string    `json:"rounded_total" doc:"Payable total; equals grand_total when rounding is off"`
		Status          string    `json:"status" enum:"Draft,Submitted,Cancelled"`
		WorkflowState   string    `json:"workflow_state"`
	}
}

type submitInput struct {
	DocType string `path:"doctype" enum:"sales-order,delivery-note,sales-invoice,payment-entry"`
	Name    string `path:"name"`
}

type submitOutput struct {
	Body struct {
		Submitted     bool   `json:"submitted" doc:"False when the action only moved the document through an approval workflow"`
		WorkflowState string `json:"workflow_state"`
	}
}

var pathDocTypes = map[string]erp.DocType{
	"sales-order":   erp.DocSalesOrder,
	"delivery-note": erp.DocDeliveryNote,
	"sales-invoice": erp.DocSalesInvoice,
	"payment-entry": erp.DocPaymentEntry,
}

func (g *gateway) registerSales(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-sales-order",
		Method:        http.MethodPost,
		Path:          "/town/api/sales-orders",
		Summary:       "Create a draft Sales Order",
		Tags:          []string{"sales"},
		DefaultStatus: http.StatusCreated,
		Errors:        writeErrors,
	}, func(ctx context.Context, in *createSalesOrderInput) (*createdOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		so, err := salesOrderInput(in)
		if err != nil {
			return nil, err
		}
		ref, err := g.port.CreateSalesOrder(ctx, p, so)
		if err != nil {
			return nil, fail(ctx, err)
		}
		out := &createdOutput{}
		out.Body.Name = ref.Name
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-sales-order",
		Method:      http.MethodGet,
		Path:        "/town/api/sales-orders/{name}",
		Summary:     "Get a Sales Order",
		Tags:        []string{"sales"},
		Errors:      readErrors,
	}, func(ctx context.Context, in *struct {
		Name string `path:"name"`
	},
	) (*salesOrderOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		so, err := g.port.GetSalesOrder(ctx, p, in.Name)
		if err != nil {
			return nil, fail(ctx, err)
		}
		return salesOrderBody(so), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "submit-document",
		Method:      http.MethodPost,
		Path:        "/town/api/documents/{doctype}/{name}/submit",
		Summary:     "Submit a document (or take its approval workflow step)",
		Tags:        []string{"documents"},
		Errors:      writeErrors,
	}, func(ctx context.Context, in *submitInput) (*submitOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		dt, ok := pathDocTypes[in.DocType]
		if !ok { // unreachable: the enum is validated first
			return nil, fail(ctx, erp.NewValidation("path.doctype", "unsupported doctype "+in.DocType))
		}
		res, err := g.port.Submit(ctx, p, erp.DocRef{DocType: dt, Name: in.Name})
		if err != nil {
			return nil, fail(ctx, err)
		}
		out := &submitOutput{}
		out.Body.Submitted = res.Submitted
		out.Body.WorkflowState = res.WorkflowState
		return out, nil
	})
}

// salesOrderInput converts the validated body; values the schema already
// checked can still fail the stricter domain parsers (e.g. > 28 digits).
func salesOrderInput(in *createSalesOrderInput) (erp.SalesOrderInput, error) {
	fields := map[string]string{}
	date := func(loc, s string) erp.Date {
		d, err := erp.ParseDate(s)
		if err != nil {
			fields[loc] = "expected a date YYYY-MM-DD"
		}
		return d
	}
	dec := func(loc, s string) erp.Decimal {
		d, err := erp.ParseDecimal(s)
		if err != nil {
			fields[loc] = "expected a decimal string of at most 28 digits"
		}
		return d
	}
	b := in.Body
	out := erp.SalesOrderInput{
		Company:         b.Company,
		Customer:        b.Customer,
		TransactionDate: date("body.transaction_date", b.TransactionDate),
		DeliveryDate:    date("body.delivery_date", b.DeliveryDate),
		Lines:           make([]erp.LineInput, len(b.Lines)),
	}
	for i, l := range b.Lines {
		out.Lines[i] = erp.LineInput{ItemCode: l.ItemCode, Qty: dec(fmt.Sprintf("body.lines[%d].qty", i), l.Qty)}
		if l.Rate != nil {
			r := dec(fmt.Sprintf("body.lines[%d].rate", i), *l.Rate)
			out.Lines[i].Rate = &r
		}
	}
	if len(fields) > 0 {
		return erp.SalesOrderInput{}, &APIError{
			Status: http.StatusUnprocessableEntity, Code: CodeValidation, Message: "validation failed", Fields: fields,
		}
	}
	return out, nil
}

func salesOrderBody(so erp.SalesOrder) *salesOrderOutput {
	out := &salesOrderOutput{}
	b := &out.Body
	b.Name = so.Ref.Name
	b.Company = so.Company
	b.Customer = so.Customer
	b.TransactionDate = dateOrNil(so.TransactionDate)
	b.DeliveryDate = dateOrNil(so.DeliveryDate)
	b.Lines = make([]lineOut, len(so.Lines))
	for i, l := range so.Lines {
		b.Lines[i] = lineOut{ItemCode: l.ItemCode, Qty: l.Qty.String(), Rate: l.Rate.String(), Amount: l.Amount.String()}
	}
	b.NetTotal = so.NetTotal.String()
	b.TaxTotal = so.TaxTotal.String()
	b.GrandTotal = so.GrandTotal.String()
	b.RoundedTotal = so.RoundedTotal.String()
	b.Status = string(so.Status)
	b.WorkflowState = so.WorkflowState
	return out
}

// dateOrNil encodes a zero date as JSON null, like erp.Date.MarshalJSON.
func dateOrNil(d erp.Date) *string {
	if d.IsZero() {
		return nil
	}
	s := d.String()
	return &s
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
