package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/atipongsena/erp-town/go/internal/erp/erpnext"
	"github.com/atipongsena/erp-town/go/internal/gateway"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store"
	"github.com/atipongsena/erp-town/go/internal/world"
	"github.com/redis/go-redis/v9"
)

// wireExperience registers the production host entry points without provisioning
// accounts or migrating an existing database. The operator opts in explicitly.
func wireExperience(ctx context.Context, mux *http.ServeMux, cfg platform.Config) (func(context.Context) error, func(), error) {
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	var provisioned bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('avatar_preferences') IS NOT NULL").Scan(&provisioned); err != nil || !provisioned {
		pool.Close()
		return nil, nil, errors.New("experience requires explicitly provisioned avatar schema")
	}
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		pool.Close()
		return nil, nil, errors.New("experience requires a valid Redis URL")
	}
	rdb := redis.NewClient(opts)
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		pool.Close()
		return nil, nil, errors.New("experience history stream unavailable")
	}
	port := erpnext.NewAdapter(erpnext.NewClient(erpnext.ClientConfig{BaseURL: cfg.ERPURL, Site: cfg.ERPSite}))
	service := gateway.RegisterExperience(mux, pool, port, world.NewRedisHistory(rdb, cfg.Stream))
	return service.Run, func() { _ = rdb.Close(); pool.Close() }, nil
}
