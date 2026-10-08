package erpnext

import (
	"context"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// CreateDeliveryNote maps the Sales Order's open quantities into a draft
// (make_delivery_note) and inserts it.
func (a *Adapter) CreateDeliveryNote(ctx context.Context, as erp.Principal, salesOrder erp.DocRef, postingDate *erp.Date) (erp.DocRef, error) {
	if salesOrder.DocType != erp.DocSalesOrder {
		return erp.DocRef{}, erp.NewValidation("sales_order", "reference is not a Sales Order")
	}
	var draft rawDoc
	args := map[string]any{"source_name": salesOrder.Name}
	if err := a.c.Call(ctx, as, "erpnext.selling.doctype.sales_order.sales_order.make_delivery_note", args, &draft); err != nil {
		return erp.DocRef{}, err
	}
	if err := setPosting(draft, postingDate); err != nil {
		return erp.DocRef{}, err
	}
	return a.insert(ctx, as, erp.DocDeliveryNote, draft)
}

type deliveryNoteDoc struct {
	Name        string   `json:"name"`
	DocStatus   int      `json:"docstatus"`
	Company     string   `json:"company"`
	Customer    string   `json:"customer"`
	PostingDate erp.Date `json:"posting_date"`
	Items       []struct {
		lineRow
		AgainstSalesOrder string `json:"against_sales_order"`
	} `json:"items"`
}

func (a *Adapter) GetDeliveryNote(ctx context.Context, as erp.Principal, name string) (erp.DeliveryNote, error) {
	var d deliveryNoteDoc
	if err := a.getDoc(ctx, as, erp.DocRef{DocType: erp.DocDeliveryNote, Name: name}, &d); err != nil {
		return erp.DeliveryNote{}, err
	}
	status, err := docStatus(d.DocStatus)
	if err != nil {
		return erp.DeliveryNote{}, err
	}
	company, err := a.companyAbbr(ctx, as)(d.Company)
	if err != nil {
		return erp.DeliveryNote{}, err
	}
	lines := make([]lineRow, 0, len(d.Items))
	orders := []string{}
	for _, it := range d.Items {
		lines = append(lines, it.lineRow)
		orders = appendUnique(orders, it.AgainstSalesOrder)
	}
	return erp.DeliveryNote{
		Ref:                erp.DocRef{DocType: erp.DocDeliveryNote, Name: d.Name},
		Company:            company,
		Customer:           d.Customer,
		PostingDate:        d.PostingDate,
		Lines:              mapLines(lines),
		Status:             status,
		AgainstSalesOrders: orders,
	}, nil
}
