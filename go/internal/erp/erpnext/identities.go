package erpnext

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// automaticRoles are roles Frappe gives every (desk) user; they say nothing
// about what the user may do in the ERP.
var automaticRoles = []string{"All", "Guest", "Desk User"}

// Whoami asks erp_town.session_api.whoami who cred belongs to. It is a GET,
// which Frappe never CSRF-checks, so a bare session id works; the answer's
// CSRF token is put into the returned SessionCredential for later writes.
// Guest (403) and an expired session (401) are ErrUnauthenticated.
func (a *Adapter) Whoami(ctx context.Context, cred erp.Credential) (erp.Identity, error) {
	var out struct {
		User      string   `json:"user"`
		Roles     []string `json:"roles"`
		Companies []string `json:"companies"`
		CSRFToken string   `json:"csrf_token"`
	}
	err := a.c.Get(ctx, erp.Principal{Credential: cred}, "/api/method/erp_town.session_api.whoami", nil, &out)
	if errors.Is(err, erp.ErrForbidden) {
		return erp.Identity{}, fmt.Errorf("%w: %s", erp.ErrUnauthenticated, errorText(err))
	}
	if err != nil {
		return erp.Identity{}, err
	}
	if out.User == "" || out.User == "Guest" {
		return erp.Identity{}, fmt.Errorf("%w: no user for this credential", erp.ErrUnauthenticated)
	}
	roles := []string{}
	for _, r := range out.Roles {
		if !slices.Contains(automaticRoles, r) {
			roles = append(roles, r)
		}
	}
	if sc, ok := cred.(erp.SessionCredential); ok {
		sc.CSRFToken = out.CSRFToken
		cred = sc
	}
	companies := out.Companies
	if companies == nil {
		companies = []string{}
	}
	return erp.Identity{User: out.User, Roles: roles, Companies: companies, Credential: cred}, nil
}
