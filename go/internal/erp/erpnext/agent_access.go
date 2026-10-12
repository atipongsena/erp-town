package erpnext

import (
	"context"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

func (a *Adapter) AgentAccess(ctx context.Context, cred erp.Credential) (erp.AgentAccess, error) {
	id, err := a.Whoami(ctx, cred)
	if err != nil {
		return erp.AgentAccess{}, err
	}
	var out struct {
		User        string `json:"user"`
		AgentKey    string `json:"agent_key"`
		Permissions map[erp.DocType]struct {
			Read   bool `json:"read"`
			Create bool `json:"create"`
		} `json:"permissions"`
	}
	err = a.c.Get(ctx, erp.Principal{Credential: id.Credential}, "/api/method/erp_town.session_api.agent_access", nil, &out)
	if err != nil {
		return erp.AgentAccess{}, err
	}
	if out.User != id.User {
		return erp.AgentAccess{}, erp.ErrUnauthenticated
	}
	perms := map[erp.DocType]erp.DocumentPermission{}
	for dt, p := range out.Permissions {
		perms[dt] = erp.DocumentPermission{Read: p.Read, Create: p.Create}
	}
	return erp.AgentAccess{Identity: id, AgentKey: out.AgentKey, Permissions: perms}, nil
}

func (a *Adapter) CheckDraftAccess(ctx context.Context, cred erp.Credential, in erp.DraftAccessInput) error {
	id, err := a.Whoami(ctx, cred)
	if err != nil {
		return err
	}
	var out struct {
		Allowed bool `json:"allowed"`
	}
	if err := a.c.post(ctx, id.Credential, "", "erp_town.session_api.draft_access", in, &out); err != nil {
		return err
	}
	if !out.Allowed {
		return erp.ErrForbidden
	}
	return nil
}
