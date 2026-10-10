package world

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Snapshot is a durable checkpoint at a fully applied stream prefix.
type Snapshot struct {
	State    State
	StreamID string
	TakenAt  time.Time
}

// SnapshotStore retains one latest internal checkpoint per company.
type SnapshotStore struct{ pool *pgxpool.Pool }

func NewSnapshotStore(pool *pgxpool.Pool) *SnapshotStore { return &SnapshotStore{pool: pool} }

func (s *SnapshotStore) Load(ctx context.Context, company string) (Snapshot, bool, error) {
	var raw []byte
	var result Snapshot
	err := s.pool.QueryRow(ctx, "SELECT stream_id, state, taken_at FROM world_snapshots WHERE company=$1", company).Scan(&result.StreamID, &raw, &result.TakenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("world: load snapshot: %w", err)
	}
	if err := decodeStoredSnapshot(company, raw, &result); err != nil {
		return Snapshot{}, false, err
	}
	return result, true, nil
}

func (s *SnapshotStore) List(ctx context.Context) ([]Snapshot, error) {
	rows, err := s.pool.Query(ctx, "SELECT company, stream_id, state, taken_at FROM world_snapshots ORDER BY company")
	if err != nil {
		return nil, fmt.Errorf("world: list snapshots: %w", err)
	}
	defer rows.Close()
	result := make([]Snapshot, 0)
	for rows.Next() {
		var company string
		var raw []byte
		var snapshot Snapshot
		if err := rows.Scan(&company, &snapshot.StreamID, &raw, &snapshot.TakenAt); err != nil {
			return nil, err
		}
		if err := decodeStoredSnapshot(company, raw, &snapshot); err != nil {
			return nil, err
		}
		result = append(result, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// Save serializes writers for the company and refuses stale cursors or room
// versions. Repeating the same checkpoint is safe; reconciliation may advance
// the room version without advancing the stream cursor.
func (s *SnapshotStore) Save(ctx context.Context, snapshot Snapshot) error {
	position, err := parseSnapshotStreamID(snapshot.StreamID)
	if err != nil {
		return err
	}
	if snapshot.TakenAt.IsZero() {
		return errors.New("world: snapshot time is required")
	}
	raw, err := EncodeSnapshot(snapshot.State)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("world: begin snapshot save: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	// The transaction lock also covers the absent-row case on the first save.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "world.snapshot/"+snapshot.State.Company()); err != nil {
		return err
	}
	var old Snapshot
	var oldRaw []byte
	err = tx.QueryRow(ctx, "SELECT stream_id, state, taken_at FROM world_snapshots WHERE company=$1", snapshot.State.Company()).Scan(&old.StreamID, &oldRaw, &old.TakenAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		if err := decodeStoredSnapshot(snapshot.State.Company(), oldRaw, &old); err != nil {
			return err
		}
		oldPosition, err := parseSnapshotStreamID(old.StreamID)
		if err != nil {
			return err
		}
		canonical, err := EncodeSnapshot(old.State)
		if err != nil {
			return err
		}
		if position.less(oldPosition) || snapshot.State.Version() < old.State.Version() {
			return errors.New("world: snapshot regression: cursor or room version is older")
		}
		if snapshot.State.Version() == old.State.Version() && !bytes.Equal(raw, canonical) {
			return errors.New("world: snapshot conflict: unchanged room version has different state")
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO world_snapshots (company, stream_id, state, taken_at) VALUES ($1,$2,$3,$4)
ON CONFLICT (company) DO UPDATE SET stream_id=EXCLUDED.stream_id, state=EXCLUDED.state, taken_at=EXCLUDED.taken_at`, snapshot.State.Company(), snapshot.StreamID, raw, snapshot.TakenAt); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("world: commit snapshot: %w", err)
	}
	return nil
}

func decodeStoredSnapshot(company string, raw []byte, snapshot *Snapshot) error {
	state, err := DecodeSnapshot(raw)
	if err != nil {
		return err
	}
	if state.Company() != company {
		return errors.New("world: snapshot company does not match storage key")
	}
	if _, err := parseSnapshotStreamID(snapshot.StreamID); err != nil {
		return err
	}
	if snapshot.TakenAt.IsZero() {
		return errors.New("world: stored snapshot has no time")
	}
	snapshot.State = state
	snapshot.TakenAt = snapshot.TakenAt.UTC()
	return nil
}

type snapshotStreamPosition struct{ ms, seq uint64 }

func (p snapshotStreamPosition) less(other snapshotStreamPosition) bool {
	return p.ms < other.ms || p.ms == other.ms && p.seq < other.seq
}

func parseSnapshotStreamID(id string) (snapshotStreamPosition, error) {
	parts := strings.Split(id, "-")
	if len(parts) != 2 {
		return snapshotStreamPosition{}, errors.New("world: invalid snapshot stream ID")
	}
	var values [2]uint64
	for i, part := range parts {
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil || strconv.FormatUint(value, 10) != part {
			return snapshotStreamPosition{}, errors.New("world: invalid snapshot stream ID")
		}
		values[i] = value
	}
	return snapshotStreamPosition{ms: values[0], seq: values[1]}, nil
}
