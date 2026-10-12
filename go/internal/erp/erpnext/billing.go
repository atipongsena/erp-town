package erpnext

import (
	"context"
	"slices"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// CreateSalesInvoice maps the Delivery Note into a draft (make_sales_invoice)
// and inserts it. With a posting date the due date moves along, keeping the
// draft's credit days, and ERPNext rebuilds the payment schedule.
func (a *Adapter) CreateSalesInvoice(ctx context.Context, as erp.Principal, deliveryNote erp.DocRef, postingDate *erp.Date) (erp.DocRef, error) {
	if deliveryNote.DocType != erp.DocDeliveryNote {
		return erp.DocRef{}, erp.NewValidation("delivery_note", "reference is not a Delivery Note")
	}
	var draft rawDoc
	args := map[string]any{"source_name": deliveryNote.Name}
	if err := a.c.Call(ctx, as, "erpnext.stock.doctype.delivery_note.delivery_note.make_sales_invoice", args, &draft); err != nil {
		return erp.DocRef{}, err
	}
	if postingDate != nil && !postingDate.IsZero() {
		if err := shiftDueDate(draft, *postingDate); err != nil {
			return erp.DocRef{}, err
		}
		if err := setPosting(draft, postingDate); err != nil {
			return erp.DocRef{}, err
		}
	}
	return a.insert(ctx, as, erp.DocSalesInvoice, draft)
}

func shiftDueDate(draft rawDoc, posting erp.Date) error {
	from, errFrom := erp.ParseDate(draft.str("posting_date"))
	due, errDue := erp.ParseDate(draft.str("due_date"))
	if errFrom == nil && errDue == nil {
		credit := int(dateTime(due).Sub(dateTime(from)).Hours() / 24)
		if err := draft.set("due_date", posting.AddDays(credit).String()); err != nil {
			return err
		}
	}
	return draft.set("payment_schedule", []any{})
}

func dateTime(d erp.Date) time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

type invoiceLineRow struct {
	lineRow
	DeliveryNote string `json:"delivery_note"`
}
type salesInvoiceDoc struct {
	Name                string           `json:"name"`
	DocStatus           int              `json:"docstatus"`
	Company             string           `json:"company"`
	Customer            string           `json:"customer"`
	PostingDate         erp.Date         `json:"posting_date"`
	DueDate             erp.Date         `json:"due_date"`
	IsReturn            int              `json:"is_return"`
	NetTotal            erp.Decimal      `json:"net_total"`
	TaxTotal            erp.Decimal      `json:"total_taxes_and_charges"`
	GrandTotal          erp.Decimal      `json:"grand_total"`
	RoundedTotal        erp.Decimal      `json:"rounded_total"`
	DisableRoundedTotal int              `json:"disable_rounded_total"`
	Outstanding         erp.Decimal      `json:"outstanding_amount"`
	Items               []invoiceLineRow `json:"items"`
}

func (a *Adapter) GetSalesInvoice(ctx context.Context, as erp.Principal, name string) (erp.SalesInvoice, error) {
	var d salesInvoiceDoc
	if err := a.getDoc(ctx, as, erp.DocRef{DocType: erp.DocSalesInvoice, Name: name}, &d); err != nil {
		return erp.SalesInvoice{}, err
	}
	status, err := docStatus(d.DocStatus)
	if err != nil {
		return erp.SalesInvoice{}, err
	}
	company, err := a.companyAbbr(ctx, as)(d.Company)
	if err != nil {
		return erp.SalesInvoice{}, err
	}
	rows := make([]lineRow, 0, len(d.Items))
	refs := []string{}
	for _, row := range d.Items {
		rows = append(rows, row.lineRow)
		if row.DeliveryNote != "" && !slices.Contains(refs, row.DeliveryNote) {
			refs = append(refs, row.DeliveryNote)
		}
	}
	return erp.SalesInvoice{
		AgainstDeliveryNotes: refs,
		Ref:                  erp.DocRef{DocType: erp.DocSalesInvoice, Name: d.Name},
		Company:              company,
		Customer:             d.Customer,
		PostingDate:          d.PostingDate,
		DueDate:              d.DueDate,
		Lines:                mapLines(rows),
		NetTotal:             d.NetTotal,
		TaxTotal:             d.TaxTotal,
		GrandTotal:           d.GrandTotal,
		RoundedTotal:         payable(d.GrandTotal, d.RoundedTotal, d.DisableRoundedTotal),
		Outstanding:          d.Outstanding,
		Status:               status,
		IsReturn:             d.IsReturn != 0,
	}, nil
}

// CreatePayment drafts a Payment Entry for the invoice's outstanding amount
// (get_payment_entry), books it to the mode of payment's account for the
// company (get_bank_cash_account, as Desk does when the mode is chosen) and
// inserts it.
func (a *Adapter) CreatePayment(ctx context.Context, as erp.Principal, in erp.PaymentInput) (erp.DocRef, error) {
	if in.ModeOfPayment == "" {
		return erp.DocRef{}, erp.NewValidation("mode_of_payment", "mode of payment is required")
	}
	if in.Invoice == "" {
		return erp.DocRef{}, erp.NewValidation("invoice", "invoice is required")
	}
	var draft rawDoc
	args := map[string]any{"dt": string(erp.DocSalesInvoice), "dn": in.Invoice}
	if err := a.c.Call(ctx, as, "erpnext.accounts.doctype.payment_entry.payment_entry.get_payment_entry", args, &draft); err != nil {
		return erp.DocRef{}, err
	}
	var account struct {
		Account string `json:"account"`
	}
	accArgs := map[string]any{"mode_of_payment": in.ModeOfPayment, "company": draft.str("company")}
	if err := a.c.Call(ctx, as, "erpnext.accounts.doctype.sales_invoice.sales_invoice.get_bank_cash_account", accArgs, &account); err != nil {
		return erp.DocRef{}, notFoundAsValidation(err, "mode_of_payment")
	}

	posting := draft.str("posting_date")
	if in.PostingDate != nil && !in.PostingDate.IsZero() {
		posting = in.PostingDate.String()
	}
	accountField := "paid_to" // a customer pays us: money is received into the account
	if draft.str("payment_type") == "Pay" {
		accountField = "paid_from"
	}
	for field, v := range map[string]any{
		"mode_of_payment": in.ModeOfPayment,
		accountField:      account.Account,
		"posting_date":    posting,
		"reference_no":    in.Invoice,
		"reference_date":  posting,
	} {
		if err := draft.set(field, v); err != nil {
			return erp.DocRef{}, err
		}
	}
	return a.insert(ctx, as, erp.DocPaymentEntry, draft)
}

type paymentEntryDoc struct {
	Name          string      `json:"name"`
	DocStatus     int         `json:"docstatus"`
	Company       string      `json:"company"`
	Party         string      `json:"party"`
	PostingDate   erp.Date    `json:"posting_date"`
	PaidAmount    erp.Decimal `json:"paid_amount"`
	ModeOfPayment string      `json:"mode_of_payment"`
	References    []struct {
		DocType string `json:"reference_doctype"`
		Name    string `json:"reference_name"`
	} `json:"references"`
}

func (a *Adapter) GetPaymentEntry(ctx context.Context, as erp.Principal, name string) (erp.PaymentEntry, error) {
	var d paymentEntryDoc
	if err := a.getDoc(ctx, as, erp.DocRef{DocType: erp.DocPaymentEntry, Name: name}, &d); err != nil {
		return erp.PaymentEntry{}, err
	}
	status, err := docStatus(d.DocStatus)
	if err != nil {
		return erp.PaymentEntry{}, err
	}
	company, err := a.companyAbbr(ctx, as)(d.Company)
	if err != nil {
		return erp.PaymentEntry{}, err
	}
	invoices := []string{}
	for _, r := range d.References {
		if r.DocType == string(erp.DocSalesInvoice) {
			invoices = appendUnique(invoices, r.Name)
		}
	}
	return erp.PaymentEntry{
		Ref:             erp.DocRef{DocType: erp.DocPaymentEntry, Name: d.Name},
		Company:         company,
		Party:           d.Party,
		PostingDate:     d.PostingDate,
		Paid:            d.PaidAmount,
		ModeOfPayment:   d.ModeOfPayment,
		Status:          status,
		AgainstInvoices: invoices,
	}, nil
}
