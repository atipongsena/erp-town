// Package auth authenticates gateway requests from the Frappe session cookie
// and issues short-lived Ed25519 JWTs for WebSocket/API use.
package auth

import (
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

type cacheEntry struct {
	identity erp.Identity
	expires  time.Time
}

// Authenticator resolves a request's Frappe session (cookie sid) to a
// Principal, caching successful whoami results per session id.
type Authenticator struct {
	ids erp.Identities
	ttl time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewAuthenticator returns an Authenticator caching identities for ttl.
func NewAuthenticator(ids erp.Identities, ttl time.Duration) *Authenticator {
	return &Authenticator{ids: ids, ttl: ttl, cache: make(map[string]cacheEntry)}
}

// FromRequest returns the user Principal for the request's sid cookie. A
// missing, empty or Guest session yields erp.ErrUnauthenticated.
func (a *Authenticator) FromRequest(r *http.Request) (erp.Principal, error) {
	c, err := r.Cookie("sid")
	if err != nil || c.Value == "" || c.Value == "Guest" {
		return erp.Principal{}, erp.ErrUnauthenticated
	}
	sid := c.Value

	if id, ok := a.lookup(sid); ok {
		return principal(id), nil
	}

	sc := erp.SessionCredential{SessionID: sid}
	id, err := a.ids.Whoami(r.Context(), sc)
	if err != nil {
		return erp.Principal{}, err
	}
	if id.User == "" {
		return erp.Principal{}, erp.ErrUnauthenticated
	}
	if id.Credential == nil {
		id.Credential = sc
	}
	a.store(sid, id)
	return principal(id), nil
}

func (a *Authenticator) lookup(sid string) (erp.Identity, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.cache[sid]
	if !ok || !time.Now().Before(e.expires) {
		return erp.Identity{}, false
	}
	return e.identity, true
}

func (a *Authenticator) store(sid string, id erp.Identity) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, e := range a.cache {
		if !now.Before(e.expires) {
			delete(a.cache, k)
		}
	}
	id.Roles = slices.Clone(id.Roles)
	id.Companies = slices.Clone(id.Companies)
	a.cache[sid] = cacheEntry{identity: id, expires: now.Add(a.ttl)}
}

// principal builds a Principal with its own copies of the slices.
func principal(id erp.Identity) erp.Principal {
	return erp.Principal{
		Kind:       erp.PrincipalUser,
		ID:         id.User,
		Roles:      slices.Clone(id.Roles),
		Companies:  slices.Clone(id.Companies),
		Credential: id.Credential,
	}
}
