// Package devbridge accepts only coarse local developer task state. It has no
// ERP imports, credentials, model calls or repository access.
package devbridge

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrRejected = errors.New("devbridge: task rejected")

type (
	Mapping struct{ Company, Actor string }
	Task    struct {
		Provider string `json:"provider"`
		TaskID   string `json:"task_id"`
		Sequence int64  `json:"sequence"`
		State    string `json:"state"`
	}
	Bridge struct {
		pool      *pgxpool.Pool
		token     []byte
		mapping   map[string]Mapping
		validator *events.Validator
	}
)

func opaque(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func New(pool *pgxpool.Pool, token string, mapping map[string]Mapping) (*Bridge, error) {
	if pool == nil || len(token) < 32 || len(token) > 256 || len(mapping) == 0 {
		return nil, ErrRejected
	}
	for provider, m := range mapping {
		if !slices.Contains([]string{"claude", "codex"}, provider) || m.Company == "" || len(m.Company) > 140 || !strings.HasPrefix(m.Actor, "agent.") || len(m.Actor) > 140 {
			return nil, ErrRejected
		}
	}
	v, err := events.NewValidator()
	if err != nil {
		return nil, err
	}
	return &Bridge{pool: pool, token: []byte(token), mapping: maps.Clone(mapping), validator: v}, nil
}

func (b *Bridge) Accept(ctx context.Context, in Task) (bool, error) {
	m, ok := b.mapping[in.Provider]
	if !ok || !opaque(in.TaskID) || in.Sequence < 1 || in.Sequence > 9007199254740991 || !slices.Contains([]string{"started", "running", "completed", "failed"}, in.State) {
		return false, ErrRejected
	}
	subject := in.Provider + "/" + in.TaskID
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return false, ErrRejected
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "devbridge/"+subject); err != nil {
		return false, ErrRejected
	}
	var raw string
	err = tx.QueryRow(ctx, "SELECT payload FROM town_outbox WHERE subject_doctype='Town Dev Task' AND subject_name=$1 ORDER BY insert_order DESC LIMIT 1", subject).Scan(&raw)
	if err == nil {
		old, err := events.Decode(b.validator, []byte(raw))
		if err != nil || old.Company == nil || *old.Company != m.Company || old.Actor.ID != m.Actor {
			return false, ErrRejected
		}
		var data struct {
			State    string `json:"state"`
			Sequence int64  `json:"sequence"`
		}
		if json.Unmarshal(old.Data, &data) != nil {
			return false, ErrRejected
		}
		if in.Sequence == data.Sequence && in.State != data.State {
			return false, ErrRejected
		}
		if in.Sequence <= data.Sequence || data.State == "completed" || data.State == "failed" {
			return false, nil
		}
		if in.State == "started" {
			return false, ErrRejected
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM town_subject_seq WHERE subject_doctype='Town Dev Task' AND subject_name=$1)", subject).Scan(&exists); err != nil || exists || in.Sequence != 1 || in.State != "started" {
			return false, ErrRejected
		}
	} else {
		return false, ErrRejected
	}
	typ := "dev.task." + in.State
	if in.State == "running" {
		typ = "dev.task.updated"
	}
	_, err = events.Emit(ctx, tx, b.validator, time.Now(), events.EmitInput{Type: typ, Source: "town/devbridge", Company: &m.Company, Actor: events.Actor{Kind: "agent", ID: m.Actor}, Subject: events.Subject{DocType: "Town Dev Task", Name: subject}, CorrelationID: in.TaskID, Visibility: events.Visibility{Level: "roles", Roles: []string{"System Manager"}}, Data: map[string]any{"provider": in.Provider, "state": in.State, "sequence": in.Sequence}})
	if err != nil {
		return false, ErrRejected
	}
	if tx.Commit(ctx) != nil {
		return false, ErrRejected
	}
	return true, nil
}

func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/dev/task" {
		http.NotFound(w, r)
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if r.Header.Get("Authorization") != "Bearer "+token || subtle.ConstantTimeCompare([]byte(token), b.token) != 1 {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	var in Task
	if decodeTask(w, r, &in) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	accepted, err := b.Accept(ctx, in)
	if err != nil {
		http.Error(w, "task rejected", http.StatusConflict)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"accepted": accepted})
}

func decodeTask(w http.ResponseWriter, r *http.Request, out *Task) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return ErrRejected
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return err
		}
		s, ok := key.(string)
		if !ok {
			return ErrRejected
		}
		if _, exists := fields[s]; exists {
			return ErrRejected
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return err
		}
		if bytes.Equal(raw, []byte("null")) {
			return ErrRejected
		}
		fields[s] = raw
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrRejected
	}
	if len(fields) != 4 {
		return ErrRejected
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	typed := json.NewDecoder(bytes.NewReader(raw))
	typed.DisallowUnknownFields()
	return typed.Decode(out)
}

// Serve requires a literal loopback bind and graceful shutdown. No other town
// routes share this listener; callers provision its token explicitly.
func Serve(ctx context.Context, addr string, bridge *Bridge) error {
	host, _, err := net.SplitHostPort(addr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || bridge == nil {
		return ErrRejected
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return ErrRejected
	}
	server := &http.Server{Handler: bridge, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return ErrRejected
	case <-ctx.Done():
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := server.Shutdown(cleanup)
		if err != nil {
			_ = server.Close()
		}
		<-done
		return err
	}
}
