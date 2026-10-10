package erp

import (
	"context"
	"fmt"
	"strings"
)

// Projection is the complete capability available to the world service. It
// exposes only company-bound display metadata and cannot perform ERP writes.
type Projection interface {
	ReadProjection(ctx context.Context, as Principal, company string) (ProjectionSnapshot, error)
}

type ProjectionSnapshot struct {
	Company   string
	Facts     []ProjectionFact
	Sequences []ProjectionSequence
}

// ProjectionFact deliberately has no money, raw document or party fields.
type ProjectionFact struct {
	Ref                 DocRef
	Kind, Scope, Status string
	FillRatio           Decimal
	DueDate             Date
	DaysOverdue         int
}

type ProjectionSequence struct {
	Ref DocRef
	Seq int64
}

func ValidateProjectionPrincipal(as Principal, company string) error {
	if as.Credential == nil {
		return ErrUnauthenticated
	}
	if as.Kind != PrincipalSystem || as.ID != "system.town" || as.OnBehalfOf != "" {
		return ErrForbidden
	}
	if strings.TrimSpace(company) == "" {
		return NewValidation("company", "company is required")
	}
	return nil
}

// Validate prevents partial or inconsistent rebuilds from becoming room state.
func (s ProjectionSnapshot) Validate(company string) error {
	if s.Company != company || strings.TrimSpace(company) == "" || s.Facts == nil || s.Sequences == nil {
		return fmt.Errorf("erp: incomplete or mismatched projection")
	}
	sequences := map[DocRef]bool{}
	validRef := func(ref DocRef) bool {
		return strings.TrimSpace(ref.Name) != "" && (ref.DocType == DocSalesOrder || ref.DocType == DocDeliveryNote || ref.DocType == DocSalesInvoice || ref.DocType == "Item")
	}
	for _, seq := range s.Sequences {
		if !validRef(seq.Ref) || seq.Seq < 0 || sequences[seq.Ref] {
			return fmt.Errorf("erp: invalid projection sequence")
		}
		sequences[seq.Ref] = true
	}
	type key struct {
		ref         DocRef
		kind, scope string
	}
	seen := map[key]bool{}
	for _, fact := range s.Facts {
		k := key{fact.Ref, fact.Kind, fact.Scope}
		if !validRef(fact.Ref) || !sequences[fact.Ref] || seen[k] {
			return fmt.Errorf("erp: invalid or unfenced projection fact")
		}
		seen[k] = true
		valid := false
		switch fact.Kind {
		case "document":
			valid = (fact.Ref.DocType == DocSalesOrder || fact.Ref.DocType == DocDeliveryNote) && fact.Scope == "" && strings.TrimSpace(fact.Status) != "" && fact.FillRatio.IsZero() && fact.DueDate.IsZero() && fact.DaysOverdue == 0
		case "overdue":
			_, err := ParseDate(fact.DueDate.String())
			valid = fact.Ref.DocType == DocSalesInvoice && fact.Scope == "" && fact.Status == "" && fact.FillRatio.IsZero() && err == nil && fact.DaysOverdue >= 1
		case "stock":
			valid = fact.Ref.DocType == "Item" && strings.TrimSpace(fact.Scope) != "" && fact.Status == "" && fact.DueDate.IsZero() && fact.DaysOverdue == 0 && fact.FillRatio.Cmp(Decimal{}) >= 0 && fact.FillRatio.Cmp(MustDecimal("1")) <= 0
		}
		if !valid {
			return fmt.Errorf("erp: invalid projection %s fact", fact.Kind)
		}
	}
	return nil
}
