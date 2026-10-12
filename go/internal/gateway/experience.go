package gateway

import (
	"net/http"
	"time"

	"github.com/atipongsena/erp-town/go/internal/agents"
	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/avatar"
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/world"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegisterExperience is opt-in on an explicitly provisioned host. It neither
// runs migrations nor provisions credentials. Presence has a separate ephemeral
// channel; the durable world projection and replay never include human movement.
func RegisterExperience(mux *http.ServeMux, pool *pgxpool.Pool, port erp.Port, history world.HistorySource) *avatar.Service {
	fresh := auth.NewAuthenticator(port, 0)
	resolve := func(r *http.Request) (erp.Principal, error) {
		p, err := fresh.FromRequest(r)
		if err != nil {
			return erp.Principal{}, err
		}
		authority, err := port.AgentAccess(r.Context(), p.Credential)
		if err != nil || authority.AgentKey != "" || authority.Identity.User != p.ID {
			return erp.Principal{}, erp.ErrForbidden
		}
		p.Roles = authority.Identity.Roles
		p.Companies = authority.Identity.Companies
		return p, nil
	}
	service := avatar.NewService(pool, port)
	avatar.NewHandler(service, resolve).Register(mux)
	mux.Handle("GET /town/api/agent-runs", agents.NewRunHistoryHandler(pool, resolve))
	if history != nil {
		mux.Handle("GET /town/api/history", world.NewHistoryHandler(world.NewHistory(history), func(r *http.Request) (auth.Claims, error) {
			p, err := resolve(r)
			if err != nil {
				return auth.Claims{}, err
			}
			return auth.Claims{Subject: p.ID, Roles: p.Roles, Companies: p.Companies, ExpiresAt: time.Now().Add(5 * time.Second)}, nil
		}))
	}
	return service
}
