package erpnext

import (
	"context"
	"encoding/json"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

const salesTaxTemplate = "Sales Taxes and Charges Template"

// CreateSalesOrder inserts a draft Sales Order the way Desk fills one: the
// customer's price list (erpnext.accounts.party.get_party_details) and the
// company's default sales tax template with its rows
// (get_default_taxes_and_charges); ERPNext's insert itself expands neither.
func (a *Adapter) CreateSalesOrder(ctx context.Context, as erp.Principal, in erp.SalesOrderInput) (erp.DocRef, error) {
	if in.Customer == "" {
		return erp.DocRef{}, erp.NewValidation("customer", "customer is required")
	}
	if len(in.Lines) == 0 {
		return erp.DocRef{}, erp.NewValidation("items", "at least one line is required")
	}
	company, err := a.companyName(ctx, as, in.Company)
	if err != nil {
		return erp.DocRef{}, err
	}

	partyArgs := map[string]any{
		"party": in.Customer, "party_type": "Customer", "company": company, "doctype": string(erp.DocSalesOrder),
	}
	if !in.TransactionDate.IsZero() {
		partyArgs["posting_date"] = in.TransactionDate.String()
	}
	var party struct {
		SellingPriceList string `json:"selling_price_list"`
	}
	if err := a.c.Call(ctx, as, "erpnext.accounts.party.get_party_details", partyArgs, &party); err != nil {
		return erp.DocRef{}, notFoundAsValidation(err, "customer")
	}

	var taxes struct {
		Template string            `json:"taxes_and_charges"`
		Rows     []json.RawMessage `json:"taxes"`
	}
	taxArgs := map[string]any{"master_doctype": salesTaxTemplate, "company": company}
	if err := a.c.Call(ctx, as, "erpnext.controllers.accounts_controller.get_default_taxes_and_charges", taxArgs, &taxes); err != nil {
		return erp.DocRef{}, err
	}

	items := make([]map[string]any, 0, len(in.Lines))
	for _, l := range in.Lines {
		row := map[string]any{"item_code": l.ItemCode, "qty": number(l.Qty)}
		if l.Rate != nil {
			row["rate"] = number(*l.Rate)
		}
		items = append(items, row)
	}
	doc := map[string]any{
		"company":    company,
		"customer":   in.Customer,
		"order_type": "Sales",
		"items":      items,
	}
	if !in.TransactionDate.IsZero() {
		doc["transaction_date"] = in.TransactionDate.String()
	}
	if !in.DeliveryDate.IsZero() {
		doc["delivery_date"] = in.DeliveryDate.String()
	}
	if party.SellingPriceList != "" {
		doc["selling_price_list"] = party.SellingPriceList
	}
	if taxes.Template != "" {
		doc["taxes_and_charges"] = taxes.Template
		doc["taxes"] = taxes.Rows
	}
	return a.insert(ctx, as, erp.DocSalesOrder, doc)
}

// number sends a Decimal as an exact JSON number (Frappe float fields).
func number(d erp.Decimal) json.Number { return json.Number(d.String()) }

type salesOrderDoc struct {
	Name                string      `json:"name"`
	DocStatus           int         `json:"docstatus"`
	WorkflowState       string      `json:"workflow_state"`
	Company             string      `json:"company"`
	Customer            string      `json:"customer"`
	TransactionDate     erp.Date    `json:"transaction_date"`
	DeliveryDate        erp.Date    `json:"delivery_date"`
	NetTotal            erp.Decimal `json:"net_total"`
	TaxTotal            erp.Decimal `json:"total_taxes_and_charges"`
	GrandTotal          erp.Decimal `json:"grand_total"`
	RoundedTotal        erp.Decimal `json:"rounded_total"`
	DisableRoundedTotal int         `json:"disable_rounded_total"`
	Items               []lineRow   `json:"items"`
}

func (a *Adapter) GetSalesOrder(ctx context.Context, as erp.Principal, name string) (erp.SalesOrder, error) {
	var d salesOrderDoc
	if err := a.getDoc(ctx, as, erp.DocRef{DocType: erp.DocSalesOrder, Name: name}, &d); err != nil {
		return erp.SalesOrder{}, err
	}
	return mapSalesOrder(d, a.companyAbbr(ctx, as))
}

func mapSalesOrder(d salesOrderDoc, abbr func(string) (string, error)) (erp.SalesOrder, error) {
	status, err := docStatus(d.DocStatus)
	if err != nil {
		return erp.SalesOrder{}, err
	}
	company, err := abbr(d.Company)
	if err != nil {
		return erp.SalesOrder{}, err
	}
	return erp.SalesOrder{
		Ref:             erp.DocRef{DocType: erp.DocSalesOrder, Name: d.Name},
		Company:         company,
		Customer:        d.Customer,
		TransactionDate: d.TransactionDate,
		DeliveryDate:    d.DeliveryDate,
		Lines:           mapLines(d.Items),
		NetTotal:        d.NetTotal,
		TaxTotal:        d.TaxTotal,
		GrandTotal:      d.GrandTotal,
		RoundedTotal:    payable(d.GrandTotal, d.RoundedTotal, d.DisableRoundedTotal),
		Status:          status,
		WorkflowState:   d.WorkflowState,
	}, nil
}
