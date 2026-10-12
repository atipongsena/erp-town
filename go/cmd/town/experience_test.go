package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/atipongsena/erp-town/go/internal/events/eventstest"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
	"github.com/stretchr/testify/require"
)

func TestGatewayReportsOnlyConfiguredWorldCapability(t *testing.T) {
	erp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/method/erp_town.session_api.whoami", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"user":"human@example.test","roles":[],"companies":["ACME-TH"]}}`))
	}))
	defer erp.Close()
	for _, world := range []bool{false, true} {
		mux := platform.NewMux()
		require.NoError(t, wireGateway(mux, platform.Config{ERPURL: erp.URL, JWTPrivateKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)), Roles: platform.Roles{platform.RoleGateway: true, platform.RoleWorld: world}}))
		guest := httptest.NewRecorder()
		mux.ServeHTTP(guest, httptest.NewRequest(http.MethodGet, "/town/api/me", nil))
		require.Equal(t, http.StatusUnauthorized, guest.Code)
		r := httptest.NewRequest(http.MethodGet, "/town/api/me", nil)
		r.AddCookie(&http.Cookie{Name: "sid", Value: "fixture-human-session"})
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, r)
		require.Equal(t, http.StatusOK, out.Code)
		var result struct {
			WorldEnabled *bool `json:"world_enabled"`
		}
		require.NoError(t, json.Unmarshal(out.Body.Bytes(), &result))
		require.NotNil(t, result.WorldEnabled)
		require.Equal(t, world, *result.WorldEnabled)
	}
}

func TestWireExperienceRequiresProvisionedSchemaWithoutMigrating(t *testing.T) {
	pool := storetest.NewEmptyPool(t)
	dbURL, err := url.Parse(pool.Config().ConnString())
	require.NoError(t, err)
	query := dbURL.Query()
	query.Set("search_path", pool.Config().ConnConfig.RuntimeParams["search_path"])
	dbURL.RawQuery = query.Encode()
	_, _, err = wireExperience(t.Context(), platform.NewMux(), platform.Config{DatabaseURL: dbURL.String()})
	require.ErrorContains(t, err, "explicitly provisioned")
	var exists bool
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT to_regclass('avatar_preferences') IS NOT NULL").Scan(&exists))
	require.False(t, exists)
}

func TestWireExperienceServesFreshHumanPreferenceHistoryAndShutsDown(t *testing.T) {
	pool := storetest.NewPool(t)
	rdb, stream := eventstest.NewRedis(t)
	dbURL, err := url.Parse(pool.Config().ConnString())
	require.NoError(t, err)
	query := dbURL.Query()
	query.Set("search_path", pool.Config().ConnConfig.RuntimeParams["search_path"])
	dbURL.RawQuery = query.Encode()
	erp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "test.localhost", r.Header.Get("X-Frappe-Site-Name"))
		cookie, err := r.Cookie("sid")
		require.NoError(t, err)
		require.Equal(t, "fixture-human-session", cookie.Value)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"user":"human@example.test","roles":["Sales User"],"companies":["ACME-TH"],"permissions":{}}}`))
	}))
	defer erp.Close()
	mux := platform.NewMux()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	run, closeHost, err := wireExperience(ctx, mux, platform.Config{DatabaseURL: dbURL.String(), RedisURL: "redis://" + rdb.Options().Addr, Stream: stream, ERPURL: erp.URL, ERPSite: "test.localhost"})
	require.NoError(t, err)
	defer closeHost()
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	for _, path := range []string{"/town/api/avatar/preference?company=ACME-TH", "/town/api/history?company=ACME-TH&from=" + url.QueryEscape(time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)) + "&until=" + url.QueryEscape(time.Now().UTC().Format(time.RFC3339)) + "&limit=10"} {
		guest := httptest.NewRecorder()
		mux.ServeHTTP(guest, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusUnauthorized, guest.Code)
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: "sid", Value: "fixture-human-session"})
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, r)
		require.Equal(t, http.StatusOK, out.Code, out.Body.String())
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
		var body map[string]any
		require.NoError(t, json.Unmarshal(out.Body.Bytes(), &body))
		if _, ok := body["consent"]; ok {
			require.Equal(t, false, body["consent"])
		}
	}
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("presence shutdown timed out")
	}
}
