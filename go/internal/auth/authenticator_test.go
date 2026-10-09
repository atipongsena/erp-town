package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

type countingIdentities struct {
	calls atomic.Int32
	err   error
}

func (c *countingIdentities) Whoami(_ context.Context, cred erp.Credential) (erp.Identity, error) {
	c.calls.Add(1)
	if c.err != nil {
		return erp.Identity{}, c.err
	}
	sc := cred.(erp.SessionCredential)
	return erp.Identity{
		User:       "alice@example.com",
		Roles:      []string{"Sales User"},
		Companies:  []string{"TOWN"},
		Credential: erp.SessionCredential{SessionID: sc.SessionID, CSRFToken: "csrf-1"},
	}, nil
}

func requestWithSID(sid string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/town/api/x", nil)
	if sid != "" {
		r.AddCookie(&http.Cookie{Name: "sid", Value: sid})
	}
	return r
}

func TestFromRequestWithoutCookieIsUnauthenticated(t *testing.T) {
	ids := &countingIdentities{}
	a := NewAuthenticator(ids, 30*time.Second)
	for _, sid := range []string{"", "Guest"} {
		_, err := a.FromRequest(requestWithSID(sid))
		require.ErrorIs(t, err, erp.ErrUnauthenticated, "sid=%q", sid)
	}
	assert.Zero(t, ids.calls.Load())
}

func TestFromRequestCachesWhoamiFor30s(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ids := &countingIdentities{}
		a := NewAuthenticator(ids, 30*time.Second)

		p, err := a.FromRequest(requestWithSID("abc"))
		require.NoError(t, err)
		assert.Equal(t, erp.PrincipalUser, p.Kind)
		assert.Equal(t, "alice@example.com", p.ID)
		assert.Equal(t, []string{"Sales User"}, p.Roles)
		assert.Equal(t, []string{"TOWN"}, p.Companies)
		assert.Equal(t, erp.SessionCredential{SessionID: "abc", CSRFToken: "csrf-1"}, p.Credential)

		// A caller mutating its copy must not corrupt the cache.
		p.Roles[0] = "Hacker"
		p.Companies[0] = "EVIL"

		time.Sleep(29 * time.Second)
		p, err = a.FromRequest(requestWithSID("abc"))
		require.NoError(t, err)
		assert.Equal(t, []string{"Sales User"}, p.Roles)
		assert.Equal(t, []string{"TOWN"}, p.Companies)
		assert.EqualValues(t, 1, ids.calls.Load())

		time.Sleep(2 * time.Second)
		_, err = a.FromRequest(requestWithSID("abc"))
		require.NoError(t, err)
		assert.EqualValues(t, 2, ids.calls.Load())
	})
}

func TestExpiredSessionIsUnauthenticated(t *testing.T) {
	ids := &countingIdentities{err: erp.ErrUnauthenticated}
	a := NewAuthenticator(ids, 30*time.Second)
	for range 2 {
		_, err := a.FromRequest(requestWithSID("dead"))
		require.ErrorIs(t, err, erp.ErrUnauthenticated)
	}
	assert.EqualValues(t, 2, ids.calls.Load(), "failures are not cached")
}

func TestUnavailableIsReturnedAndNotCached(t *testing.T) {
	ids := &countingIdentities{err: erp.ErrUnavailable}
	a := NewAuthenticator(ids, 30*time.Second)
	_, err := a.FromRequest(requestWithSID("abc"))
	require.ErrorIs(t, err, erp.ErrUnavailable)
	ids.err = nil
	_, err = a.FromRequest(requestWithSID("abc"))
	require.NoError(t, err)
}

func TestExpiredEntriesArePruned(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a := NewAuthenticator(&countingIdentities{}, 30*time.Second)
		for _, sid := range []string{"a", "b", "c"} {
			_, err := a.FromRequest(requestWithSID(sid))
			require.NoError(t, err)
		}
		time.Sleep(31 * time.Second)
		_, err := a.FromRequest(requestWithSID("d"))
		require.NoError(t, err)
		a.mu.Lock()
		defer a.mu.Unlock()
		assert.Len(t, a.cache, 1)
	})
}
