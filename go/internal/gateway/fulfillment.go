package gateway

import (
	"context"
	"net/http"
	"slices"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/danielgtaylor/huma/v2"
)

type deliveryInput struct {
	Body struct {
		Company     string  `json:"company" minLength:"1"`
		SalesOrder  string  `json:"sales_order" minLength:"1"`
		PostingDate *string `json:"posting_date,omitempty" format:"date"`
	}
}
type invoiceInput struct {
	Body struct {
		Company      string  `json:"company" minLength:"1"`
		DeliveryNote string  `json:"delivery_note" minLength:"1"`
		PostingDate  *string `json:"posting_date,omitempty" format:"date"`
	}
}
type paymentInput struct {
	Body struct {
		Company       string  `json:"company" minLength:"1"`
		SalesInvoice  string  `json:"sales_invoice" minLength:"1"`
		ModeOfPayment string  `json:"mode_of_payment" minLength:"1"`
		PostingDate   *string `json:"posting_date,omitempty" format:"date"`
	}
}
type (
	documentNameInput struct {
		Name string `path:"name"`
	}
	deliveryOutput struct {
		Body struct {
			Name               string    `json:"name"`
			Company            string    `json:"company"`
			Customer           string    `json:"customer"`
			PostingDate        *string   `json:"posting_date" format:"date"`
			Lines              []lineOut `json:"lines"`
			Status             string    `json:"status" enum:"Draft,Submitted,Cancelled"`
			AgainstSalesOrders []string  `json:"against_sales_orders"`
		}
	}
)

type invoiceOutput struct {
	Body struct {
		AgainstDeliveryNotes []string  `json:"against_delivery_notes"`
		Name                 string    `json:"name"`
		Company              string    `json:"company"`
		Customer             string    `json:"customer"`
		PostingDate          *string   `json:"posting_date" format:"date"`
		DueDate              *string   `json:"due_date" format:"date"`
		Lines                []lineOut `json:"lines"`
		NetTotal             string    `json:"net_total"`
		TaxTotal             string    `json:"tax_total"`
		GrandTotal           string    `json:"grand_total"`
		RoundedTotal         string    `json:"rounded_total"`
		Outstanding          string    `json:"outstanding"`
		Status               string    `json:"status" enum:"Draft,Submitted,Cancelled"`
		IsReturn             bool      `json:"is_return"`
	}
}
type paymentOutput struct {
	Body struct {
		Name            string   `json:"name"`
		Company         string   `json:"company"`
		Party           string   `json:"party"`
		PostingDate     *string  `json:"posting_date" format:"date"`
		Paid            string   `json:"paid"`
		ModeOfPayment   string   `json:"mode_of_payment"`
		Status          string   `json:"status" enum:"Draft,Submitted,Cancelled"`
		AgainstInvoices []string `json:"against_invoices"`
	}
}

func (g *gateway) registerFulfillment(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "create-delivery-note", Method: http.MethodPost, Path: "/town/api/delivery-notes",
		Summary: "Create a draft Delivery Note from a submitted Sales Order",
		Tags:    []string{"delivery"}, DefaultStatus: http.StatusCreated, Errors: writeErrors,
	}, func(ctx context.Context, in *deliveryInput) (*createdOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		if !slices.Contains(p.Companies, in.Body.Company) {
			return nil, fail(ctx, erp.ErrForbidden)
		}
		date, err := postingDate(in.Body.PostingDate)
		if err != nil {
			return nil, fail(ctx, err)
		}
		source, err := g.port.GetSalesOrder(ctx, p, in.Body.SalesOrder)
		if err != nil {
			return nil, fail(ctx, err)
		}
		if err := fulfillmentSource(in.Body.Company, source.Company, source.Status); err != nil {
			return nil, fail(ctx, err)
		}
		ref, err := g.port.CreateDeliveryNote(ctx, p, source.Ref, date)
		return created(ctx, ref, err)
	})
	huma.Register(api, huma.Operation{
		OperationID: "get-delivery-note", Method: http.MethodGet, Path: "/town/api/delivery-notes/{name}",
		Summary: "Get a Delivery Note", Tags: []string{"delivery"}, Errors: readErrors,
	}, func(ctx context.Context, in *documentNameInput) (*deliveryOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		doc, err := g.port.GetDeliveryNote(ctx, p, in.Name)
		if err != nil {
			return nil, fail(ctx, err)
		}
		out := &deliveryOutput{}
		out.Body.Name, out.Body.Company, out.Body.Customer = doc.Ref.Name, doc.Company, doc.Customer
		out.Body.PostingDate, out.Body.Lines = dateOrNil(doc.PostingDate), outputLines(doc.Lines)
		out.Body.Status, out.Body.AgainstSalesOrders = string(doc.Status), nonNil(doc.AgainstSalesOrders)
		return out, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "create-sales-invoice", Method: http.MethodPost, Path: "/town/api/sales-invoices",
		Summary: "Create a draft Sales Invoice from a submitted Delivery Note",
		Tags:    []string{"billing"}, DefaultStatus: http.StatusCreated, Errors: writeErrors,
	}, func(ctx context.Context, in *invoiceInput) (*createdOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		if !slices.Contains(p.Companies, in.Body.Company) {
			return nil, fail(ctx, erp.ErrForbidden)
		}
		date, err := postingDate(in.Body.PostingDate)
		if err != nil {
			return nil, fail(ctx, err)
		}
		source, err := g.port.GetDeliveryNote(ctx, p, in.Body.DeliveryNote)
		if err != nil {
			return nil, fail(ctx, err)
		}
		if err := fulfillmentSource(in.Body.Company, source.Company, source.Status); err != nil {
			return nil, fail(ctx, err)
		}
		ref, err := g.port.CreateSalesInvoice(ctx, p, source.Ref, date)
		return created(ctx, ref, err)
	})
	huma.Register(api, huma.Operation{
		OperationID: "get-sales-invoice", Method: http.MethodGet, Path: "/town/api/sales-invoices/{name}",
		Summary: "Get a Sales Invoice", Tags: []string{"billing"}, Errors: readErrors,
	}, func(ctx context.Context, in *documentNameInput) (*invoiceOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		doc, err := g.port.GetSalesInvoice(ctx, p, in.Name)
		if err != nil {
			return nil, fail(ctx, err)
		}
		out := &invoiceOutput{}
		b := &out.Body
		b.Name, b.Company, b.Customer = doc.Ref.Name, doc.Company, doc.Customer
		b.PostingDate, b.DueDate, b.Lines = dateOrNil(doc.PostingDate), dateOrNil(doc.DueDate), outputLines(doc.Lines)
		b.NetTotal, b.TaxTotal = doc.NetTotal.String(), doc.TaxTotal.String()
		b.GrandTotal, b.RoundedTotal, b.Outstanding = doc.GrandTotal.String(), doc.RoundedTotal.String(), doc.Outstanding.String()
		b.Status, b.IsReturn = string(doc.Status), doc.IsReturn
		b.AgainstDeliveryNotes = append([]string{}, doc.AgainstDeliveryNotes...)
		return out, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "create-payment-entry", Method: http.MethodPost, Path: "/town/api/payment-entries",
		Summary: "Create a draft receipt for the full outstanding invoice balance",
		Tags:    []string{"billing"}, DefaultStatus: http.StatusCreated, Errors: writeErrors,
	}, func(ctx context.Context, in *paymentInput) (*createdOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		if !slices.Contains(p.Companies, in.Body.Company) {
			return nil, fail(ctx, erp.ErrForbidden)
		}
		date, err := postingDate(in.Body.PostingDate)
		if err != nil {
			return nil, fail(ctx, err)
		}
		source, err := g.port.GetSalesInvoice(ctx, p, in.Body.SalesInvoice)
		if err != nil {
			return nil, fail(ctx, err)
		}
		if err := fulfillmentSource(in.Body.Company, source.Company, source.Status); err != nil {
			return nil, fail(ctx, err)
		}
		ref, err := g.port.CreatePayment(ctx, p, erp.PaymentInput{
			Invoice: source.Ref.Name, ModeOfPayment: in.Body.ModeOfPayment, PostingDate: date,
		})
		return created(ctx, ref, err)
	})
	huma.Register(api, huma.Operation{
		OperationID: "get-payment-entry", Method: http.MethodGet, Path: "/town/api/payment-entries/{name}",
		Summary: "Get a Payment Entry", Tags: []string{"billing"}, Errors: readErrors,
	}, func(ctx context.Context, in *documentNameInput) (*paymentOutput, error) {
		p, err := principal(ctx)
		if err != nil {
			return nil, fail(ctx, err)
		}
		doc, err := g.port.GetPaymentEntry(ctx, p, in.Name)
		if err != nil {
			return nil, fail(ctx, err)
		}
		out := &paymentOutput{}
		b := &out.Body
		b.Name, b.Company, b.Party = doc.Ref.Name, doc.Company, doc.Party
		b.PostingDate, b.Paid = dateOrNil(doc.PostingDate), doc.Paid.String()
		b.ModeOfPayment, b.Status, b.AgainstInvoices = doc.ModeOfPayment, string(doc.Status), nonNil(doc.AgainstInvoices)
		return out, nil
	})
}

func fulfillmentSource(selected, actual string, status erp.DocStatus) error {
	if selected != actual {
		return erp.ErrForbidden
	}
	if status != erp.DocSubmitted {
		return erp.NewValidation("source", "source document must be submitted")
	}
	return nil
}

func postingDate(raw *string) (*erp.Date, error) {
	if raw == nil {
		return nil, nil
	}
	d, err := erp.ParseDate(*raw)
	if err != nil {
		return nil, erp.NewValidation("body.posting_date", "expected a date YYYY-MM-DD")
	}
	return &d, nil
}

func created(ctx context.Context, ref erp.DocRef, err error) (*createdOutput, error) {
	if err != nil {
		return nil, fail(ctx, err)
	}
	out := &createdOutput{}
	out.Body.Name = ref.Name
	return out, nil
}

func outputLines(lines []erp.Line) []lineOut {
	out := make([]lineOut, len(lines))
	for i, l := range lines {
		out[i] = lineOut{ItemCode: l.ItemCode, Qty: l.Qty.String(), Rate: l.Rate.String(), Amount: l.Amount.String()}
	}
	return out
}
