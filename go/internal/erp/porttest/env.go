// Package porttest is the conformance suite every erp.Port adapter must pass:
// the in-memory fake, the ERPNext adapter and a future Go core. It speaks
// domain terms only; every concrete value comes from Env.
package porttest

import "github.com/atipongsena/erp-town/go/internal/erp"

// Env describes the backend under test.
//
// Company is the company abbr; Customer has no credit limit; LimitedCustomer
// is over its credit limit when it orders BigQty of BigItem, which sends the
// Sales Order into PendingState until an approver applies ApproveAction.
// Sales, Approver, Stock and Accounts carry working credentials. Today is the
// date documents are created on.
type Env struct {
	Port                                              erp.Port
	Company, Customer, LimitedCustomer, Item, BigItem string
	ModeOfPayment, PendingState, ApproveAction        string
	BigQty                                            erp.Decimal
	Sales, Approver, Stock, Accounts                  erp.Principal
	Today                                             erp.Date
}
