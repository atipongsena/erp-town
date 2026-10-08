package erpfake

import (
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/porttest"
)

// DefaultEnv is a fake seeded like the ERPNext test site (company ACME-TH,
// customers CUST-001..010, CUST-009 limited to 5000, ITEM-002 at 12 and
// ITEM-018 at 4500) together with the matching porttest.Env.
func DefaultEnv() porttest.Env {
	const company = "ACME-TH"
	users := map[string]FakeUser{
		"sess-sales":    {User: "sales@acme-th.test", Roles: []string{RoleSalesUser}, Companies: []string{company}, CSRFToken: "csrf-sales"},
		"sess-stock":    {User: "stock@acme-th.test", Roles: []string{RoleStockUser}, Companies: []string{company}, CSRFToken: "csrf-stock"},
		"sess-accounts": {User: "accounts@acme-th.test", Roles: []string{RoleAccountsUser}, Companies: []string{company}, CSRFToken: "csrf-accounts"},
		"sess-approver": {User: "approver@acme-th.test", Roles: []string{RoleSalesManager, RoleSalesUser}, Companies: []string{company}, CSRFToken: "csrf-approver"},
	}
	f := New(Config{
		Company: company,
		Prices: map[string]erp.Decimal{
			"ITEM-002": erp.MustDecimal("12"),
			"ITEM-018": erp.MustDecimal("4500"),
		},
		CreditLimits: map[string]erp.Decimal{
			"CUST-009": erp.MustDecimal("5000"),
			"CUST-010": erp.MustDecimal("5000"),
		},
		Users: users,
	})
	principal := func(sess string) erp.Principal {
		u := users[sess]
		return erp.Principal{
			Kind: erp.PrincipalUser, ID: u.User, Roles: u.Roles, Companies: u.Companies,
			Credential: erp.SessionCredential{SessionID: sess, CSRFToken: u.CSRFToken},
		}
	}
	return porttest.Env{
		Port:            f,
		Company:         company,
		Customer:        "CUST-001",
		LimitedCustomer: "CUST-009",
		Item:            "ITEM-002",
		BigItem:         "ITEM-018",
		ItemRate:        erp.MustDecimal("12"),
		BigQty:          erp.MustDecimal("2"),
		ModeOfPayment:   "เงินสด",
		PendingState:    statePending,
		ApproveAction:   "อนุมัติ",
		Sales:           principal("sess-sales"),
		Approver:        principal("sess-approver"),
		Stock:           principal("sess-stock"),
		Accounts:        principal("sess-accounts"),
		Today:           f.cfg.Today,
	}
}
