package erp

// DocType uses the same strings as the event contract's subject.doctype.
type DocType string

const (
	DocSalesOrder   DocType = "Sales Order"
	DocDeliveryNote DocType = "Delivery Note"
	DocSalesInvoice DocType = "Sales Invoice"
	DocPaymentEntry DocType = "Payment Entry"
)

// DocRef identifies one document.
type DocRef struct {
	DocType DocType
	Name    string
}

type DocStatus string

const (
	DocDraft     DocStatus = "Draft"
	DocSubmitted DocStatus = "Submitted"
	DocCancelled DocStatus = "Cancelled"
)

// LineInput is one requested line; a nil Rate takes the price list rate.
type LineInput struct {
	ItemCode string
	Qty      Decimal
	Rate     *Decimal
}

type Line struct {
	ItemCode          string
	Qty, Rate, Amount Decimal
}

// SalesOrderInput: Company is the abbr code; the ERP's defaults fill price list,
// warehouse and the company's default sales tax template.
type SalesOrderInput struct {
	Company, Customer             string
	TransactionDate, DeliveryDate Date
	Lines                         []LineInput
}

type SalesOrder struct {
	Ref                            DocRef
	Company, Customer              string
	TransactionDate, DeliveryDate  Date
	Lines                          []Line
	NetTotal, TaxTotal, GrandTotal Decimal
	RoundedTotal                   Decimal // payable total; equals GrandTotal when rounding is off
	Status                         DocStatus
	WorkflowState                  string
}

type DeliveryNote struct {
	Ref                DocRef
	Company, Customer  string
	PostingDate        Date
	Lines              []Line
	Status             DocStatus
	AgainstSalesOrders []string
}

type SalesInvoice struct {
	Ref                                         DocRef
	Company, Customer                           string
	PostingDate, DueDate                        Date
	Lines                                       []Line
	NetTotal, TaxTotal, GrandTotal, Outstanding Decimal
	RoundedTotal                                Decimal // payable total; equals GrandTotal when rounding is off
	Status                                      DocStatus
	IsReturn                                    bool
}

// PaymentInput pays Invoice in full; a nil PostingDate means today.
type PaymentInput struct {
	Invoice, ModeOfPayment string
	PostingDate            *Date
}

type PaymentEntry struct {
	Ref             DocRef
	Company, Party  string
	PostingDate     Date
	Paid            Decimal
	ModeOfPayment   string
	Status          DocStatus
	AgainstInvoices []string
}

// SubmitResult: Submitted is false when the action only moved the document
// through an approval workflow (WorkflowState is then the new state).
type SubmitResult struct {
	Submitted     bool
	WorkflowState string
}
