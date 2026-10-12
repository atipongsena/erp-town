package erp

import "context"

// Documents covers lifecycle actions common to every doctype.
//
// Submit semantics (all adapters): no workflow -> submit; with a workflow ->
// among the transitions available to the principal take the one leading to a
// submitted state, else the single available transition (an approval request:
// Submitted=false, WorkflowState = new state), else a ValidationError whose
// Fields has key "workflow".
type Documents interface {
	Submit(ctx context.Context, as Principal, ref DocRef) (SubmitResult, error)
	Cancel(ctx context.Context, as Principal, ref DocRef) error
}

type Sales interface {
	CreateSalesOrder(ctx context.Context, as Principal, in SalesOrderInput) (DocRef, error)
	GetSalesOrder(ctx context.Context, as Principal, name string) (SalesOrder, error)
}

type Delivery interface {
	CreateDeliveryNote(ctx context.Context, as Principal, salesOrder DocRef, postingDate *Date) (DocRef, error)
	GetDeliveryNote(ctx context.Context, as Principal, name string) (DeliveryNote, error)
}

type Billing interface {
	CreateSalesInvoice(ctx context.Context, as Principal, deliveryNote DocRef, postingDate *Date) (DocRef, error)
	GetSalesInvoice(ctx context.Context, as Principal, name string) (SalesInvoice, error)
	CreatePayment(ctx context.Context, as Principal, in PaymentInput) (DocRef, error)
	GetPaymentEntry(ctx context.Context, as Principal, name string) (PaymentEntry, error)
}

// Workflow exposes approval transitions (only Sales Orders have a workflow today).
type Workflow interface {
	Actions(ctx context.Context, as Principal, ref DocRef) ([]string, error)
	Apply(ctx context.Context, as Principal, ref DocRef, action string) (SubmitResult, error)
}

// Identities resolves a credential to a user (whoami).
type Identities interface {
	Whoami(ctx context.Context, cred Credential) (Identity, error)
}

// Port is the only way Go code reaches the ERP.
type Port interface {
	Documents
	Sales
	Delivery
	Billing
	Workflow
	Identities
	AgentIdentities
}
