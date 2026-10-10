package world

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/store/storetest"
)

func TestSnapshotStorePersistsCompaniesAndRejectsRegression(t *testing.T) {
	pool := storetest.NewPool(t)
	storage := NewSnapshotStore(pool)
	ctx := t.Context()
	_, found, err := storage.Load(ctx, "ACME-TH")
	require.NoError(t, err)
	assert.False(t, found)
	p, state := setup(t)
	state = apply(t, p, state, fixture(t, "erp.sales_order.submitted"))
	taken := time.Now().UTC().Truncate(time.Microsecond)
	snapshot := Snapshot{State: state, StreamID: "100-2", TakenAt: taken}
	require.NoError(t, storage.Save(ctx, snapshot))
	restarted := NewSnapshotStore(pool)
	loaded, found, err := restarted.Load(ctx, state.Company())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, snapshot.StreamID, loaded.StreamID)
	assert.Equal(t, taken, loaded.TakenAt)
	assert.Equal(t, state.Entities(), loaded.State.Entities())
	other, err := NewState("OTHER")
	require.NoError(t, err)
	require.NoError(t, storage.Save(ctx, Snapshot{State: other, StreamID: "101-0", TakenAt: taken}))
	all, err := storage.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "ACME-TH", all[0].State.Company())
	assert.Equal(t, "OTHER", all[1].State.Company())
	for _, invalid := range []string{"99-99", "100-1", "bad", "-1-0", "1", "1-2-3", "01-0", "18446744073709551616-0"} {
		bad := snapshot
		bad.StreamID = invalid
		require.Error(t, storage.Save(ctx, bad), invalid)
	}
	oldState, err := NewState(state.Company())
	require.NoError(t, err)
	require.Error(t, storage.Save(ctx, Snapshot{State: oldState, StreamID: "102-0", TakenAt: taken}))
	conflict := snapshot
	conflict.State = state.copyForWrite()
	entity := conflict.State.entities[state.Entities()[0].Key]
	entity.Visibility.Level = "private"
	conflict.State.entities[entity.Key] = entity
	require.Error(t, storage.Save(ctx, conflict), "same cursor and version cannot hide conflicting state")
	require.NoError(t, storage.Save(ctx, snapshot), "identical checkpoint is idempotent")
	newEvent := fixture(t, "erp.sales_invoice.submitted")
	newer := Snapshot{State: apply(t, p, state, newEvent), StreamID: "100-10", TakenAt: taken.Add(time.Second)}
	reconciled := newer
	reconciled.StreamID = snapshot.StreamID
	require.NoError(t, storage.Save(ctx, reconciled), "reconciliation may advance state at the same stream cursor")
	require.NoError(t, storage.Save(ctx, newer), "cursor ordering is numeric, not lexical")
	loaded, found, err = storage.Load(ctx, state.Company())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, newer.State.Version(), loaded.State.Version())
	assert.Equal(t, newer.StreamID, loaded.StreamID)
}

func TestSnapshotStoreRejectsCorruptPersistedCompany(t *testing.T) {
	pool := storetest.NewPool(t)
	storage := NewSnapshotStore(pool)
	_, state := setup(t)
	raw, err := EncodeSnapshot(state)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), "INSERT INTO world_snapshots (company, stream_id, state, taken_at) VALUES ($1, $2, $3, $4)", "WRONG", "1-0", raw, time.Now())
	require.NoError(t, err)
	_, _, err = storage.Load(t.Context(), "WRONG")
	require.Error(t, err)
	_, err = storage.List(t.Context())
	require.Error(t, err)
}

func TestSnapshotStoreConcurrentWritersRetainNewestCheckpoint(t *testing.T) {
	pool := storetest.NewPool(t)
	storage := NewSnapshotStore(pool)
	p, state := setup(t)
	event := fixture(t, "erp.sales_order.submitted")
	const writers = 8
	checkpoints := make([]Snapshot, writers)
	for i := range checkpoints {
		event.Seq = int64(i + 1)
		state = apply(t, p, state, event)
		checkpoints[i] = Snapshot{State: state, StreamID: fmt.Sprintf("5-%d", i+1), TakenAt: time.Now()}
	}
	start := make(chan struct{})
	results := make(chan error, writers)
	for _, checkpoint := range checkpoints {
		go func() {
			<-start
			results <- storage.Save(t.Context(), checkpoint)
		}()
	}
	close(start)
	for range writers {
		if err := <-results; err != nil {
			require.True(t, strings.Contains(err.Error(), "snapshot regression"), err)
		}
	}
	latest, found, err := storage.Load(t.Context(), state.Company())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, uint64(writers), latest.State.Version())
	assert.Equal(t, "5-8", latest.StreamID)
}
