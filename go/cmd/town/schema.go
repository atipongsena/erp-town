package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/store"
)

func prepareSchema(ctx context.Context, cfg platform.Config, pool *pgxpool.Pool) error {
	if cfg.RequireProvisionedSchema {
		return store.RequireCurrentSchema(ctx, pool)
	}
	return store.Migrate(ctx, pool)
}
