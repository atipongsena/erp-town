//go:build integration

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/erpnext"
	"github.com/atipongsena/erp-town/go/internal/gateway"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
	"github.com/atipongsena/erp-town/go/internal/world"
)

func TestClientLive(t *testing.T) {
	e := testEnv(t)
	require.Equal(t, "test.localhost", e.site)
	require.Equal(t, "town:test:events", e.stream)
	require.Equal(t, "http://127.0.0.1:8080", e.erpURL)
	require.Equal(t, "redis://127.0.0.1:6380/0", e.redisURL)
	key, secret := os.Getenv("TOWN_TEST_WORLD_KEY"), os.Getenv("TOWN_TEST_WORLD_SECRET")
	if key == "" || secret == "" {
		t.Skip("run node tests/integration/client-acceptance.ts after building the client")
	}
	root, err := filepath.Abs("../../..")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(root, "packages/town-client/dist/index.html"))
	require.NoError(t, err, "run pnpm build:client first")
	client := erpnext.NewClient(erpnext.ClientConfig{BaseURL: e.erpURL, Site: e.site})
	adapter := erpnext.NewAdapter(client)
	reader := erp.Principal{Kind: erp.PrincipalSystem, ID: "system.town", Credential: erp.APIKeyCredential{Key: key, Secret: secret}}
	projection, err := adapter.ReadProjection(t.Context(), reader, "ACME-TH")
	require.NoError(t, err, "real dedicated reader permission check")
	require.Equal(t, "ACME-TH", projection.Company)
	_, err = adapter.ReadProjection(t.Context(), reader, "ETT")
	require.ErrorIs(t, err, erp.ErrForbidden, "company restriction is enforced by ERP")
	for _, doctype := range []string{"Sales Order", "Delivery Note", "Sales Invoice", "Payment Entry"} {
		err = client.Insert(t.Context(), reader, doctype, map[string]any{"company": "ACME-TH", "customer": "CUST-001"}, nil)
		require.ErrorIs(t, err, erp.ErrForbidden, "reader cannot create %s", doctype)
	}

	seed := make([]byte, 32)
	_, err = rand.Read(seed)
	require.NoError(t, err)
	issuer, err := auth.NewJWTIssuer(base64.StdEncoding.EncodeToString(seed), 30*time.Second)
	require.NoError(t, err)
	options, err := redis.ParseURL(e.redisURL)
	require.NoError(t, err)
	rdb := redis.NewClient(options)
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	pool := storetest.NewPool(t)
	ctx, cancel := context.WithCancel(t.Context())
	service := world.NewService(ctx)
	m, _ := platform.NewTestMetrics()
	cfg := world.DefaultRuntimeConfig(e.stream)
	cfg.Consumer.Group = "client-" + randomHex(t, 6)
	cfg.Consumer.Name = "browser"
	runtime, err := world.NewRuntime(service, rdb, pool, m, adapter, reader, cfg)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case runErr := <-done:
			if runErr != nil && !errors.Is(runErr, context.Canceled) {
				t.Errorf("world shutdown: %v", runErr)
			}
		case <-time.After(15 * time.Second):
			t.Error("world shutdown timed out")
		}
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		require.NoError(t, rdb.XGroupDestroy(cleanup, e.stream, cfg.Consumer.Group).Err())
	})
	ready, stop := context.WithTimeout(t.Context(), 30*time.Second)
	require.NoError(t, runtime.BeforeJoin(ready, "ACME-TH"))
	stop()
	mux := platform.NewMux()
	gateway.New(mux, adapter, auth.NewAuthenticator(adapter, 30*time.Second), issuer)
	avatars := gateway.RegisterExperience(mux, pool, adapter, world.NewRedisHistory(rdb, e.stream))
	avatarDone := make(chan error, 1)
	go func() { avatarDone <- avatars.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-avatarDone:
			require.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Error("avatar shutdown timed out")
		}
	})
	ws := world.NewHandler(service, issuer)
	ws.BeforeJoin = runtime.BeforeJoin
	mux.Handle("GET /town/ws", ws)
	target, err := url.Parse(e.erpURL)
	require.NoError(t, err)
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		r.Out.Header.Set("X-Frappe-Site-Name", "test.localhost")
	}}
	for _, prefix := range []string{"/api/", "/app", "/app/", "/desk", "/desk/", "/files/", "/login"} {
		mux.Handle(prefix, proxy)
	}
	dist := filepath.Join(root, "packages/town-client/dist")
	static := http.FileServer(http.Dir(dist))
	// Vite and Classic ERP both use /assets/. Serve the built client files first;
	// forward only files absent from the client build to the isolated ERP site.
	mux.Handle("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, statErr := fs.Stat(os.DirFS(dist), r.URL.Path[1:])
		if statErr == nil && !info.IsDir() {
			static.ServeHTTP(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	mux.Handle("/", static)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	node := os.Getenv("TOWN_TEST_NODE")
	require.NotEmpty(t, node)
	browserCtx, stopBrowser := context.WithTimeout(t.Context(), 4*time.Minute)
	defer stopBrowser()
	command := exec.CommandContext(browserCtx, node, filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", "tests/integration/client-playwright.config.ts")
	command.Dir = root
	// The browser needs only the seeded user's password, never reader/admin API keys.
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		switch name {
		case "TOWN_TEST_WORLD_KEY", "TOWN_TEST_WORLD_SECRET", "TEST_API_KEY", "TEST_API_SECRET":
			continue
		}
		command.Env = append(command.Env, value)
	}
	command.Env = append(command.Env,
		"TOWN_CLIENT_BASE_URL=http://127.0.0.1:5197",
		"TOWN_DEV_GATEWAY_URL="+srv.URL,
		"TOWN_DEV_ERP_URL="+e.erpURL,
		"TOWN_DEV_ERP_SITE="+e.site,
		"TOWN_DEV_CACHE_DIR=node_modules/.vite-client-acceptance",
	)
	output, browserErr := command.CombinedOutput()
	t.Log(string(output))
	require.NoError(t, browserErr, "real browser acceptance")
}
