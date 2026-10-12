package world

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/events"
)

func (r *Runtime) ensureGroup(ctx context.Context) error {
	err := r.rdb.XGroupCreateMkStream(ctx, r.cfg.Consumer.Stream, r.cfg.Consumer.Group, "$").Err()
	if err != nil && !redis.HasErrorPrefix(err, "BUSYGROUP") {
		return err
	}
	return nil
}

func (r *Runtime) boundary(ctx context.Context) (string, error) {
	info, err := r.rdb.XInfoStream(ctx, r.cfg.Consumer.Stream).Result()
	if err != nil {
		return "", err
	}
	return info.LastGeneratedID, nil
}

func (r *Runtime) checkpointRetained(ctx context.Context, cursor string) (bool, error) {
	if cursor == "0-0" {
		info, err := r.rdb.XInfoStream(ctx, r.cfg.Consumer.Stream).Result()
		return err == nil && info.LastGeneratedID == "0-0", err
	}
	entries, err := r.rdb.XRangeN(ctx, r.cfg.Consumer.Stream, cursor, cursor, 1).Result()
	return len(entries) == 1, err
}

// Recover establishes live delivery before capturing its raw replay boundary.
// Consumer callbacks start only after this completes. Replay never consults
// consumed_events or consumer_subject_seq: those describe past deliveries, not
// what survived in the last in-memory checkpoint.
func (r *Runtime) Recover(ctx context.Context) (result error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() { r.recoveryErr = result; r.readyOnce.Do(func() { close(r.ready) }) }()
	if err := r.ensureGroup(ctx); err != nil {
		return err
	}
	tail, err := r.boundary(ctx)
	if err != nil {
		return err
	}
	snapshots, err := r.store.List(ctx)
	if err != nil {
		return err
	}
	for attempt := range 3 {
		err = r.recoverPrefix(ctx, snapshots, tail, attempt > 0)
		if !errors.Is(err, errReplayTrimmed) {
			return err
		}
	}
	return fmt.Errorf("world: recovery history keeps changing: %w", err)
}

var errReplayTrimmed = errors.New("world: stream was trimmed during replay")

func (r *Runtime) recoverPrefix(ctx context.Context, snapshots []Snapshot, tail string, forceRebuild bool) error {
	r.states = map[string]companyCheckpoint{}
	replayAfter := map[string]string{}
	anchors := []string{}
	for _, snapshot := range snapshots {
		company := snapshot.State.Company()
		if !r.allowsCompany(company) {
			continue
		}
		if maxStreamID(snapshot.StreamID, tail) != tail {
			return errors.New("world: stream cursor regressed; refusing to discard checkpoint history")
		}
		retained, err := r.checkpointRetained(ctx, snapshot.StreamID)
		if err != nil {
			return err
		}
		state := snapshot.State
		after := snapshot.StreamID
		if !retained || forceRebuild {
			state, err = r.rebuild(ctx, company, state)
			if err != nil {
				return err
			}
			after = "0-0"
		} else {
			anchors = append(anchors, after)
		}
		r.states[company] = companyCheckpoint{state: state, cursor: tail}
		replayAfter[company] = after
	}
	err := r.walk(ctx, "0-0", tail, func(id string, e events.Envelope) error {
		if e.Company == nil || !r.allowsCompany(*e.Company) {
			return nil
		}
		company := *e.Company
		checkpoint, exists := r.states[company]
		if !exists {
			state, err := r.rebuild(ctx, company, State{})
			if err != nil {
				return err
			}
			checkpoint = companyCheckpoint{state: state, cursor: tail}
			r.states[company] = checkpoint
			replayAfter[company] = "0-0"
		}
		if maxStreamID(id, replayAfter[company]) == replayAfter[company] {
			return nil
		}
		state, err := r.projector.Apply(checkpoint.state, e)
		if err != nil {
			return err
		}
		checkpoint.state = state
		r.states[company] = checkpoint
		return nil
	})
	if err != nil {
		return err
	}
	for _, anchor := range anchors {
		retained, err := r.checkpointRetained(ctx, anchor)
		if err != nil {
			return err
		}
		if !retained {
			return errReplayTrimmed
		}
	}
	for _, checkpoint := range r.states {
		if err := r.service.Publish(ctx, checkpoint.state); err != nil {
			return err
		}
	}
	return nil
}

// walk reads bounded pages and validates raw history without durable dedupe.
func (r *Runtime) walk(ctx context.Context, after, tail string, apply func(string, events.Envelope) error) error {
	// A retained first entry anchors the scan. Prefix trimming during a page or
	// ERP read invalidates the entire unpublished attempt, not just the cursor.
	first, err := r.rdb.XRangeN(ctx, r.cfg.Consumer.Stream, "-", tail, 1).Result()
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	cursor := after
	for maxStreamID(cursor, tail) != cursor {
		entries, err := r.rdb.XRangeN(ctx, r.cfg.Consumer.Stream, "("+cursor, tail, 100).Result()
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			cursor = entry.ID
			raw, ok := entry.Values["event"].(string)
			if !ok {
				slog.Warn("world recovery: invalid stream entry", "entry", entry.ID)
				continue
			}
			e, err := events.Decode(r.projector.validator, []byte(raw))
			if err != nil {
				slog.Warn("world recovery: invalid event skipped", "entry", entry.ID)
				continue
			}
			if _, duplicate := seen[e.ID]; duplicate {
				continue
			}
			seen[e.ID] = struct{}{}
			if err := apply(entry.ID, e); err != nil {
				return err
			}
		}
	}
	if len(first) > 0 {
		retained, err := r.checkpointRetained(ctx, first[0].ID)
		if err != nil {
			return err
		}
		if !retained {
			return errReplayTrimmed
		}
	}
	return nil
}

// ensureCompany runs only under the runtime mutex and captures a company-specific
// raw prefix. Other companies' checkpoints cannot advance to this lazy boundary.
func (r *Runtime) ensureCompany(ctx context.Context, company string) error {
	if !r.allowsCompany(company) {
		return erp.ErrForbidden
	}
	if _, exists := r.states[company]; exists {
		return nil
	}
	for range 3 {
		err := r.buildCompany(ctx, company)
		if !errors.Is(err, errReplayTrimmed) {
			return err
		}
	}
	return fmt.Errorf("world: company recovery history keeps changing: %w", errReplayTrimmed)
}

func (r *Runtime) buildCompany(ctx context.Context, company string) error {
	tail, err := r.boundary(ctx)
	if err != nil {
		return err
	}
	state, err := r.rebuild(ctx, company, State{})
	if err != nil {
		return err
	}
	err = r.walk(ctx, "0-0", tail, func(_ string, e events.Envelope) error {
		if e.Company == nil || *e.Company != company {
			return nil
		}
		var err error
		state, err = r.projector.Apply(state, e)
		return err
	})
	if err != nil {
		return err
	}
	if err := r.service.Publish(ctx, state); err != nil {
		return fmt.Errorf("world: publish rebuilt company: %w", err)
	}
	r.states[company] = companyCheckpoint{state: state, cursor: tail}
	return nil
}
