package avatar

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m, storetest.IgnoreBackgroundGoroutines()...) }

type ids struct {
	revoked bool
	err     error
}

func (i *ids) Whoami(context.Context, erp.Credential) (erp.Identity, error) {
	if i.err != nil {
		return erp.Identity{}, i.err
	}
	if i.revoked {
		return erp.Identity{User: "human@test"}, nil
	}
	return erp.Identity{User: "human@test", Companies: []string{"ACME"}}, nil
}

func TestAccountRevocationClearsConsentButTransportFailurePreservesIt(t *testing.T) {
	s, id, p, _ := fixture(t)
	for _, failure := range []error{erp.ErrUnavailable, erp.ErrForbidden, erp.ErrUnauthenticated} {
		id.err = nil
		require.NoError(t, s.SetPreference(t.Context(), p, "ACME", Preference{Consent: true, Appearance: "human-green"}))
		_, err := s.Update(t.Context(), p, "ACME", "hq", "idle")
		require.NoError(t, err)
		id.err = failure
		_, err = s.List(t.Context(), p, "ACME")
		require.ErrorIs(t, err, erp.ErrForbidden)
		require.Empty(t, s.live)
		id.err = nil
		pref, err := s.Preference(t.Context(), p, "ACME")
		require.NoError(t, err)
		require.Equal(t, failure == erp.ErrUnavailable, pref.Consent)
	}
}

func fixture(t *testing.T) (*Service, *ids, erp.Principal, *time.Time) {
	t.Helper()
	id := &ids{}
	now := time.Now()
	s := NewService(storetest.NewPool(t), id)
	s.now = func() time.Time { return now }
	return s, id, erp.Principal{Kind: erp.PrincipalUser, ID: "human@test", Credential: erp.SessionCredential{SessionID: "human"}}, &now
}

func TestConsentDefaultOffOptOutAndNoHumanTracking(t *testing.T) {
	s, _, p, _ := fixture(t)
	pref, err := s.Preference(t.Context(), p, "ACME")
	require.NoError(t, err)
	require.False(t, pref.Consent)
	_, err = s.Update(t.Context(), p, "ACME", "it", "wave")
	require.ErrorIs(t, err, erp.ErrForbidden)
	require.NoError(t, s.SetPreference(t.Context(), p, "ACME", Preference{Consent: true, Appearance: "human-blue"}))
	v, err := s.Update(t.Context(), p, "ACME", "it", "wave")
	require.NoError(t, err)
	require.NotContains(t, v.ID, "human")
	list, err := s.List(t.Context(), p, "ACME")
	require.NoError(t, err)
	require.Len(t, list, 1)
	raw, err := json.Marshal(list)
	require.NoError(t, err)
	for _, secret := range []string{"human@test", "session", "user", "xp", "level", "score", "history"} {
		require.False(t, strings.Contains(string(raw), secret))
	}
	require.NoError(t, s.SetPreference(t.Context(), p, "ACME", Preference{}))
	list, err = s.List(t.Context(), p, "ACME")
	require.NoError(t, err)
	require.Empty(t, list)
	var n int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM town_outbox").Scan(&n))
	require.Zero(t, n)
}

func TestExpiryAndRevocationErasePresenceAndPreferences(t *testing.T) {
	s, id, p, now := fixture(t)
	require.NoError(t, s.SetPreference(t.Context(), p, "ACME", Preference{Consent: true, Appearance: "human-blue"}))
	first, err := s.Update(t.Context(), p, "ACME", "shop", "idle")
	require.NoError(t, err)
	*now = now.Add(61 * time.Second)
	list, err := s.List(t.Context(), p, "ACME")
	require.NoError(t, err)
	require.Empty(t, list)
	next, err := s.Update(t.Context(), p, "ACME", "shop", "idle")
	require.NoError(t, err)
	require.NotEqual(t, first.ID, next.ID)
	id.revoked = true
	_, err = s.List(t.Context(), p, "ACME")
	require.ErrorIs(t, err, erp.ErrForbidden)
	require.Empty(t, s.live)
	var n int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM avatar_preferences").Scan(&n))
	require.Zero(t, n)
}

func TestOnlyCurrentHumanCanChangeSafeAppearance(t *testing.T) {
	s, _, p, _ := fixture(t)
	wrong := p
	wrong.ID = "someone@test"
	require.ErrorIs(t, s.SetPreference(t.Context(), wrong, "ACME", Preference{Consent: true, Appearance: "human-blue"}), erp.ErrForbidden)
	require.ErrorIs(t, s.SetPreference(t.Context(), p, "OTHER", Preference{Consent: true, Appearance: "human-blue"}), erp.ErrForbidden)
	for _, appearance := range []string{"../../file", "agent-bot", "human@test", ""} {
		require.ErrorIs(t, s.SetPreference(t.Context(), p, "ACME", Preference{Consent: true, Appearance: appearance}), erp.ErrValidation)
	}
}

func TestConsentedWorkEmotesAreEphemeralAndKeepOpaqueIdentity(t *testing.T) {
	s, _, p, now := fixture(t)
	for _, emote := range []string{"read", "type", "carry"} {
		_, err := s.Update(t.Context(), p, "ACME", "warehouse", emote)
		require.ErrorIs(t, err, erp.ErrForbidden)
	}
	require.NoError(t, s.SetPreference(t.Context(), p, "ACME", Preference{Consent: true, Appearance: "human-blue"}))
	var first string
	for _, emote := range []string{"read", "type", "carry", "idle"} {
		v, err := s.Update(t.Context(), p, "ACME", "warehouse", emote)
		require.NoError(t, err)
		if first == "" {
			first = v.ID
		}
		require.Equal(t, first, v.ID)
		require.Equal(t, emote, v.Emote)
	}
	_, err := s.Update(t.Context(), p, "ACME", "warehouse", "dispatch_payment")
	require.ErrorIs(t, err, erp.ErrValidation)
	*now = now.Add(61 * time.Second)
	list, err := s.List(t.Context(), p, "ACME")
	require.NoError(t, err)
	require.Empty(t, list)
	var count int
	require.NoError(t, s.pool.QueryRow(t.Context(), "SELECT count(*) FROM town_outbox").Scan(&count))
	require.Zero(t, count, "human work must never become a durable activity journal")
}
