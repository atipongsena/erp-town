package erpfake

import (
	"context"
	"slices"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

var _ erp.Projection = (*Fake)(nil)

func (f *Fake) ReadProjection(ctx context.Context, as erp.Principal, company string) (erp.ProjectionSnapshot, error) {
	if err := erp.ValidateProjectionPrincipal(as, company); err != nil {
		return erp.ProjectionSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return erp.ProjectionSnapshot{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u, err := f.user(as.Credential)
	if err != nil {
		return erp.ProjectionSnapshot{}, err
	}
	if company != f.cfg.Company || !slices.Contains(u.Companies, company) || !u.has("Town Projection Reader") {
		return erp.ProjectionSnapshot{}, erp.ErrForbidden
	}
	out := erp.ProjectionSnapshot{Company: company, Facts: []erp.ProjectionFact{}, Sequences: []erp.ProjectionSequence{}}
	for _, so := range f.so {
		if so.Company != company {
			continue
		}
		out.Sequences = append(out.Sequences, erp.ProjectionSequence{Ref: so.Ref}) // fake has no event emitter
		open := so.Status == erp.DocSubmitted
		for _, dn := range f.dn {
			if dn.Status == erp.DocSubmitted && slices.Contains(dn.AgainstSalesOrders, so.Ref.Name) {
				open = false
			}
		}
		if open {
			out.Facts = append(out.Facts, erp.ProjectionFact{Ref: so.Ref, Kind: "document", Status: "To Deliver"})
		}
	}
	for _, dn := range f.dn {
		if dn.Company != company {
			continue
		}
		out.Sequences = append(out.Sequences, erp.ProjectionSequence{Ref: dn.Ref})
		open := dn.Status == erp.DocSubmitted
		for _, si := range f.si {
			if si.Status == erp.DocSubmitted && si.deliveryNote == dn.Ref.Name {
				open = false
			}
		}
		if open {
			out.Facts = append(out.Facts, erp.ProjectionFact{Ref: dn.Ref, Kind: "document", Status: "To Bill"})
		}
	}
	for _, si := range f.si {
		if si.Company != company {
			continue
		}
		out.Sequences = append(out.Sequences, erp.ProjectionSequence{Ref: si.Ref})
		if si.Status == erp.DocSubmitted && !si.IsReturn && si.Outstanding.Cmp(erp.Decimal{}) > 0 && !si.DueDate.IsZero() && si.DueDate.Before(f.cfg.Today) {
			due, _ := time.Parse(time.DateOnly, si.DueDate.String())
			today, _ := time.Parse(time.DateOnly, f.cfg.Today.String())
			out.Facts = append(out.Facts, erp.ProjectionFact{Ref: si.Ref, Kind: "overdue", DueDate: si.DueDate, DaysOverdue: int(today.Sub(due) / (24 * time.Hour))})
		}
	}
	slices.SortFunc(out.Facts, func(a, b erp.ProjectionFact) int {
		return slices.Compare([]string{string(a.Ref.DocType), a.Ref.Name, a.Kind, a.Scope}, []string{string(b.Ref.DocType), b.Ref.Name, b.Kind, b.Scope})
	})
	slices.SortFunc(out.Sequences, func(a, b erp.ProjectionSequence) int {
		return slices.Compare([]string{string(a.Ref.DocType), a.Ref.Name}, []string{string(b.Ref.DocType), b.Ref.Name})
	})
	return out, out.Validate(company)
}
