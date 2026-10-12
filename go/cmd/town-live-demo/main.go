// town-live-demo is intentionally a separate loopback-only executable. It has
// no production flags, database URLs, generic credentials or mutating HTTP API.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/atipongsena/erp-town/go/internal/agents"
	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/avatar"
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/erpnext"
	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/atipongsena/erp-town/go/internal/gateway"
	"github.com/atipongsena/erp-town/go/internal/livedemo"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store"
	"github.com/atipongsena/erp-town/go/internal/world"
)

const (
	port   = "5196"
	erpURL = "http://127.0.0.1:8080"
	site   = "test.localhost"
	stream = "town:test:events"
)

type (
	credential struct{ User, Key, Secret string }
	settings   struct {
		Staff  map[string]credential `json:"staff"`
		Reader credential            `json:"reader"`
		Agent  struct {
			AgentKey  string     `json:"agent_key"`
			Agent     credential `json:"agent"`
			Delegator credential `json:"delegator"`
		} `json:"agent"`
		Viewer struct{ User, Password string } `json:"viewer"`
		Rounds int                             `json:"rounds"`
	}
)

func allowedRequest(r *http.Request, port string) bool {
	host, p, err := net.SplitHostPort(r.Host)
	if err != nil || p != port || !slices.Contains([]string{"localhost", "live-demo.localhost", "127.0.0.1"}, host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	return r.Header.Get("Origin") == "" || r.Header.Get("Origin") == "http://"+r.Host
}

func main() {
	if err := run(); err != nil {
		// ERP error bodies can contain private arguments. Expose a fixed code.
		slog.Error("live demo stopped", "code", "DEMO_HOST_FAILED")
		os.Exit(1)
	}
}

func run() error {
	// The launcher sends secret values through stdin, never flags, files or logs.
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 64*1024)
	if !scanner.Scan() {
		return errors.New("launcher configuration required")
	}
	var cfg settings
	if json.Unmarshal(scanner.Bytes(), &cfg) != nil || cfg.Reader.Key == "" || cfg.Viewer.Password == "" || cfg.Rounds < 1 || cfg.Rounds > 3 {
		return errors.New("invalid launcher configuration")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	go func() {
		for scanner.Scan() {
			if scanner.Text() == "stop" {
				cancel()
				return
			}
		}
		cancel()
	}()
	root, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	dist := filepath.Join(root, "packages/town-client/dist")
	if _, err = os.Stat(filepath.Join(dist, "index.html")); err != nil {
		return errors.New("build the client first")
	}
	startup, stopStartup := context.WithTimeout(ctx, 3*time.Minute)
	defer stopStartup()
	container, err := tcpostgres.Run(startup, "postgres:18-alpine", tcpostgres.WithDatabase("town_live_demo"), tcpostgres.WithUsername("town_demo"), tcpostgres.WithPassword("isolated-test-only"), testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(2*time.Minute)))
	if err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		_ = container.Terminate(cleanup)
	}()
	connection, err := container.ConnectionString(startup, "sslmode=disable")
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(startup, connection)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = store.Migrate(startup, pool); err != nil {
		return err
	}
	adapter := erpnext.NewAdapter(erpnext.NewClient(erpnext.ClientConfig{BaseURL: erpURL, Site: site}))
	staff := map[string]erp.Principal{}
	for d, c := range cfg.Staff {
		id, err := adapter.Whoami(startup, erp.APIKeyCredential{Key: c.Key, Secret: c.Secret})
		if err != nil {
			return err
		}
		if id.User != c.User || !slices.Contains(id.Companies, "ACME-TH") {
			return erp.ErrForbidden
		}
		staff[d] = erp.Principal{Kind: erp.PrincipalUser, ID: id.User, Roles: id.Roles, Companies: id.Companies, Credential: id.Credential}
	}
	viewerSID, err := login(startup, cfg.Viewer.User, cfg.Viewer.Password)
	if err != nil {
		return err
	}
	seed := make([]byte, 32)
	if _, err = rand.Read(seed); err != nil {
		return err
	}
	issuer, err := auth.NewJWTIssuer(base64.StdEncoding.EncodeToString(seed), 20*time.Minute)
	if err != nil {
		return err
	}
	opts, err := redis.ParseURL("redis://127.0.0.1:6380/0")
	if err != nil {
		return err
	}
	rdb := redis.NewClient(opts)
	defer func() { _ = rdb.Close() }()
	publisherRDB := events.NewPublisherRedisClient(opts)
	defer func() { _ = publisherRDB.Close() }()
	m, _ := platform.NewTestMetrics()
	service := world.NewService(ctx)
	runtimeCfg := world.DefaultRuntimeConfig(stream)
	runtimeCfg.Consumer.Group = "live-demo-" + base64.RawURLEncoding.EncodeToString(seed[:8])
	runtimeCfg.Consumer.Name = "demo"
	reader := erp.Principal{Kind: erp.PrincipalSystem, ID: "system.town", Credential: erp.APIKeyCredential{Key: cfg.Reader.Key, Secret: cfg.Reader.Secret}}
	runtime, err := world.NewRuntime(service, rdb, pool, m, adapter, reader, runtimeCfg)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = rdb.XGroupDestroy(cleanup, stream, runtimeCfg.Consumer.Group).Err()
	}()
	validator, err := events.NewValidator()
	if err != nil {
		return err
	}
	publisher := events.NewPublisher(pool, publisherRDB, validator, m, events.DefaultPublisherConfig(stream))
	ledger := agents.NewPostgresLedger(pool)
	binding := agents.Binding{AgentKey: cfg.Agent.AgentKey, Company: "ACME-TH", AgentCredential: erp.APIKeyCredential{Key: cfg.Agent.Agent.Key, Secret: cfg.Agent.Agent.Secret}, DelegatorCredential: erp.APIKeyCredential{Key: cfg.Agent.Delegator.Key, Secret: cfg.Agent.Delegator.Secret}}
	scenario, err := livedemo.New(adapter, staff, livedemo.DraftWithAgent(adapter, ledger, binding, cfg.Agent.Delegator.User), cfg.Rounds)
	if err != nil {
		return err
	}
	mux := platform.NewMux()
	avatars := registerAPI(mux, pool, adapter, issuer, service, runtime.BeforeJoin, world.NewRedisHistory(rdb, stream), scenario)
	target, _ := url.Parse(erpURL)
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(target); r.Out.Header.Set("X-Frappe-Site-Name", site) }}
	for _, prefix := range []string{"/api/", "/app", "/app/", "/desk", "/desk/", "/files/", "/login"} {
		mux.Handle(prefix, proxy)
	}
	static := http.FileServer(http.Dir(dist))
	start := make(chan struct{}, 1)
	mux.Handle("/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, statErr := fs.Stat(os.DirFS(dist), strings.TrimPrefix(r.URL.Path, "/"))
		if statErr == nil && !info.IsDir() {
			static.ServeHTTP(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && r.Method == http.MethodGet {
			// Cookies ignore ports. Use the dedicated localhost hostname so a demo
			// sign-in cannot replace the ordinary ERP/workspace account's cookie.
			if r.Host != "live-demo.localhost:"+port {
				http.Redirect(w, r, "http://live-demo.localhost:"+port+r.URL.RequestURI(), http.StatusTemporaryRedirect)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: viewerSID, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			w.Header().Set("Cache-Control", "no-store")
			select {
			case start <- struct{}{}:
			default:
			}
		}
		static.ServeHTTP(w, r)
	}))
	server := &http.Server{Addr: "127.0.0.1:" + port, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedRequest(r, port) {
			http.Error(w, "loopback demo only", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	}), ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	defer func() {
		cancel()
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = server.Shutdown(cleanup)
		wg.Wait()
	}()
	background := func(f func(context.Context) error) {
		wg.Go(func() {
			if err := f(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("demo service failed", "code", "DEMO_SERVICE_FAILED")
				cancel()
			}
		})
	}
	background(runtime.Run)
	background(avatars.Run)
	background(func(ctx context.Context) error { return publisher.Run(ctx, 250*time.Millisecond) })
	ready, stopReady := context.WithTimeout(ctx, 30*time.Second)
	err = runtime.BeforeJoin(ready, "ACME-TH")
	stopReady()
	if err != nil {
		return err
	}
	wg.Go(func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel()
		}
	})
	// A scenario failure remains visible as a stopped/error state, with no retries.
	wg.Go(func() {
		select {
		case <-ctx.Done():
			return
		case <-start:
		}
		if err := pauseStart(ctx); err == nil {
			if err := scenario.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("demo scenario stopped", "code", "ERP_STEP_FAILED")
			}
		}
	})
	slog.Info("ERP live demo ready", "url", "http://live-demo.localhost:"+port+"/?lang=th&liveDemo=1", "site", site, "rounds", cfg.Rounds)
	<-ctx.Done()
	return nil
}

func registerAPI(mux *http.ServeMux, pool *pgxpool.Pool, adapter erp.Port, issuer *auth.JWTIssuer, service *world.Service, beforeJoin func(context.Context, string) error, history world.HistorySource, scenario *livedemo.Scenario) *avatar.Service {
	authn := auth.NewAuthenticator(adapter, 5*time.Second)
	gateway.New(mux, adapter, authn, issuer, gateway.HostCapabilities{WorldEnabled: true})
	avatars := gateway.RegisterExperience(mux, pool, adapter, history)
	mux.HandleFunc("GET /town/api/live-demo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		principal, err := authn.FromRequest(r)
		if err != nil || !slices.Contains(principal.Companies, "ACME-TH") || !slices.Contains(principal.Roles, "System Manager") {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(scenario.Snapshot())
	})
	ws := world.NewHandler(service, issuer)
	ws.BeforeJoin = beforeJoin
	mux.Handle("GET /town/ws", ws)
	return avatars
}

func pauseStart(ctx context.Context) error {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func login(ctx context.Context, user, password string) (string, error) {
	raw, _ := json.Marshal(map[string]string{"usr": user, "pwd": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, erpURL+"/api/method/login", bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Frappe-Site-Name", site)
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return "", errors.New("isolated viewer login failed")
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "sid" && cookie.Value != "" && cookie.Value != "Guest" {
			return cookie.Value, nil
		}
	}
	return "", errors.New("isolated viewer session missing")
}
