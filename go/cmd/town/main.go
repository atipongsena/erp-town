// Command town is the ERP Town platform binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/erpnext"
	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/atipongsena/erp-town/go/internal/gateway"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store"
	"github.com/atipongsena/erp-town/go/internal/world"
)

const (
	whoamiCacheTTL  = 30 * time.Second
	jwtTTL          = 15 * time.Minute
	publishInterval = time.Second
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("town failed", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("usage: town serve [--roles=a,b]")
	}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	rolesFlag := fs.String("roles", "", "comma-separated roles (overrides TOWN_ROLES)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	getenv := os.Getenv
	if *rolesFlag != "" {
		getenv = func(k string) string {
			if k == "TOWN_ROLES" {
				return *rolesFlag
			}
			return os.Getenv(k)
		}
	}
	cfg, err := platform.LoadConfig(getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, cfg)
}

// serve wires the configured roles onto one mux and runs them until ctx ends
// or one of them fails. The HTTP server (healthz, metrics, and the API when
// the gateway role is on) always runs.
func serve(ctx context.Context, cfg platform.Config) error {
	metrics, metricsHandler, err := platform.NewMetrics()
	if err != nil {
		return err
	}
	mux := platform.NewMux()
	mux.Handle("GET /town/metrics", metricsHandler)

	var tasks []func(context.Context) error
	if cfg.Roles.Has(platform.RoleGateway) {
		if err := wireGateway(mux, cfg); err != nil {
			return err
		}
	}
	if cfg.Roles.Has(platform.RoleOutbox) {
		publish, closeOutbox, err := wireOutbox(ctx, cfg, metrics)
		if err != nil {
			return err
		}
		defer closeOutbox()
		tasks = append(tasks, publish)
	}
	if cfg.Roles.Has(platform.RoleWorld) {
		runWorld, closeWorld, err := wireWorld(ctx, mux, cfg, metrics)
		if err != nil {
			return err
		}
		defer closeWorld()
		tasks = append(tasks, runWorld)
	}
	tasks = append(tasks, func(ctx context.Context) error { return platform.Serve(ctx, cfg.HTTPAddr, mux) })

	slog.Info("town serving", "addr", cfg.HTTPAddr, "roles", roleNames(cfg.Roles))
	return runAll(ctx, tasks...)
}

func wireGateway(mux *http.ServeMux, cfg platform.Config) error {
	port := erpnext.NewAdapter(erpnext.NewClient(erpnext.ClientConfig{BaseURL: cfg.ERPURL, Site: cfg.ERPSite}))
	issuer, err := auth.NewJWTIssuer(cfg.JWTPrivateKey, jwtTTL)
	if err != nil {
		return err
	}
	gateway.New(mux, port, auth.NewAuthenticator(port, whoamiCacheTTL), issuer)
	return nil
}

func wireWorld(ctx context.Context, mux *http.ServeMux, cfg platform.Config, m *platform.Metrics) (func(context.Context) error, func(), error) {
	issuer, err := auth.NewJWTIssuer(cfg.JWTPrivateKey, jwtTTL)
	if err != nil {
		return nil, nil, err
	}
	if cfg.WorldAPIKey == "" || cfg.WorldAPISecret == "" {
		return nil, nil, errors.New("world requires a dedicated Town Projection Reader credential")
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	if err := store.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, err
	}
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("TOWN_REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(opts)
	service := world.NewService(ctx)
	closeAll := func() {
		service.Close()
		_ = rdb.Close()
		pool.Close()
	}
	// Adapter selection stays in cmd; the runtime receives only the read capability.
	reader := erpnext.NewAdapter(erpnext.NewClient(erpnext.ClientConfig{BaseURL: cfg.ERPURL, Site: cfg.ERPSite}))
	as := erp.Principal{Kind: erp.PrincipalSystem, ID: "system.town", Credential: erp.APIKeyCredential{Key: cfg.WorldAPIKey, Secret: cfg.WorldAPISecret}}
	runtime, err := world.NewRuntime(service, rdb, pool, m, reader, as, world.DefaultRuntimeConfig(cfg.Stream))
	if err != nil {
		closeAll()
		return nil, nil, err
	}
	handler := world.NewHandler(service, issuer)
	handler.BeforeJoin = runtime.BeforeJoin
	mux.Handle("GET /town/ws", handler)
	return runtime.Run, closeAll, nil
}

// wireOutbox opens and migrates Postgres and returns the publisher loop and a
// function that releases its connections.
func wireOutbox(ctx context.Context, cfg platform.Config, m *platform.Metrics) (func(context.Context) error, func(), error) {
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	if err := store.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, err
	}
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("TOWN_REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(opts)
	v, err := events.NewValidator()
	if err != nil {
		_ = rdb.Close()
		pool.Close()
		return nil, nil, err
	}
	pub := events.NewPublisher(pool, rdb, v, m, events.DefaultPublisherConfig(cfg.Stream))
	closeAll := func() {
		_ = rdb.Close()
		pool.Close()
	}
	return func(ctx context.Context) error { return pub.Run(ctx, publishInterval) }, closeAll, nil
}

// runAll runs every task until all return; the first error cancels the others
// and is returned.
func runAll(ctx context.Context, tasks ...func(context.Context) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	for _, task := range tasks {
		wg.Go(func() {
			if err := task(ctx); err != nil {
				once.Do(func() { firstErr = err })
				cancel(err)
			}
		})
	}
	wg.Wait()
	if firstErr != nil && !errors.Is(firstErr, context.Canceled) {
		return firstErr
	}
	return nil
}

func roleNames(r platform.Roles) []string {
	names := make([]string, 0, len(r))
	for role, on := range r {
		if on {
			names = append(names, string(role))
		}
	}
	sort.Strings(names)
	return names
}
