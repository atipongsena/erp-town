package store_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/atipongsena/erp-town/go/internal/store"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, storetest.IgnoreBackgroundGoroutines()...)
}

func TestMigrateCreatesEventTables(t *testing.T) {
	pool := storetest.NewPool(t)
	for _, table := range []string{"town_outbox", "town_subject_seq", "consumed_events", "consumer_subject_seq"} {
		var reg *string
		err := pool.QueryRow(t.Context(), "SELECT to_regclass($1)::text", table).Scan(&reg)
		require.NoError(t, err)
		require.NotNil(t, reg, "table %s missing", table)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := storetest.NewPool(t)
	require.NoError(t, store.Migrate(t.Context(), pool))
	require.NoError(t, store.Migrate(t.Context(), pool))
}

func TestRequireCurrentSchemaDoesNotProvisionAnEmptyDatabase(t *testing.T) {
	pool := storetest.NewEmptyPool(t)
	require.ErrorContains(t, store.RequireCurrentSchema(t.Context(), pool), "provisioned")
	var exists bool
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT to_regclass('goose_db_version') IS NOT NULL").Scan(&exists))
	require.False(t, exists)
	require.NoError(t, store.Migrate(t.Context(), pool))
	require.NoError(t, store.RequireCurrentSchema(t.Context(), pool))
	_, err := pool.Exec(t.Context(), "UPDATE goose_db_version SET is_applied=false WHERE version_id=5")
	require.NoError(t, err)
	require.ErrorContains(t, store.RequireCurrentSchema(t.Context(), pool), "provisioned")
	var applied bool
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT is_applied FROM goose_db_version WHERE version_id=5").Scan(&applied))
	require.False(t, applied)
}

func TestSchemasAreIsolated(t *testing.T) {
	a := storetest.NewPool(t)
	b := storetest.NewPool(t)
	_, err := a.Exec(t.Context(), "INSERT INTO town_subject_seq VALUES ('Sales Order', 'SO-1', 1)")
	require.NoError(t, err)
	var n int
	require.NoError(t, b.QueryRow(t.Context(), "SELECT count(*) FROM town_subject_seq").Scan(&n))
	require.Equal(t, 0, n)
}

func TestConcurrentMigrateIsSafe(t *testing.T) {
	// Several town processes (e.g. outbox replicas) may start at once on an
	// empty database; the advisory lock serialises them.
	const migrators = 4
	for range 3 {
		pool := storetest.NewEmptyPool(t)
		start := make(chan struct{})
		errs := make(chan error, migrators)
		for range migrators {
			go func() {
				<-start
				errs <- store.Migrate(t.Context(), pool)
			}()
		}
		close(start)
		for range migrators {
			require.NoError(t, <-errs)
		}
		var reg *string
		require.NoError(t, pool.QueryRow(t.Context(), "SELECT to_regclass('town_outbox')::text").Scan(&reg))
		require.NotNil(t, reg)
	}
}
