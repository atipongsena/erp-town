package erpfake

import (
	"context"
	"slices"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// Ordinary fake users have no agent capability. Tests explicitly configure an
// authority wrapper; role names alone must never turn a human into an agent.
func (f *Fake) AgentAccess(ctx context.Context, cred erp.Credential) (erp.AgentAccess, error) {
	id, err := f.Whoami(ctx, cred)
	if err != nil {
		return erp.AgentAccess{}, err
	}
	return erp.AgentAccess{Identity: id, Permissions: map[erp.DocType]erp.DocumentPermission{}}, nil
}

func (f *Fake) CheckDraftAccess(_ context.Context, cred erp.Credential, in erp.DraftAccessInput) error {
	if !slices.Contains([]erp.DocType{erp.DocSalesOrder, erp.DocDeliveryNote, erp.DocSalesInvoice, erp.DocPaymentEntry}, in.DocType) {
		return erp.ErrForbidden
	}
	u, err := f.user(cred)
	if err != nil {
		return err
	}
	if in.Company != f.cfg.Company || !slices.Contains(u.Companies, in.Company) || !u.has(writeRoles(in.DocType)...) {
		return erp.ErrForbidden
	}
	if in.Customer != "" {
		if _, ok := f.cfg.CreditLimits[in.Customer]; !ok {
			return erp.ErrForbidden
		}
	}
	for _, code := range in.ItemCodes {
		if _, ok := f.cfg.Prices[code]; !ok {
			return erp.ErrForbidden
		}
	}
	return nil
}
