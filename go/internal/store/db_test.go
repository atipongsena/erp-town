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

func TestSchemasAreIsolated(t *testing.T) {
	a := storetest.NewPool(t)
	b := storetest.NewPool(t)
	_, err := a.Exec(t.Context(), "INSERT INTO town_subject_seq VALUES ('Sales Order', 'SO-1', 1)")
	require.NoError(t, err)
	var n int
	require.NoError(t, b.QueryRow(t.Context(), "SELECT count(*) FROM town_subject_seq").Scan(&n))
	require.Equal(t, 0, n)
}
