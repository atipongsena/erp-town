package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/store/sqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RunSummary struct {
	ID         string `json:"id"`
	Agent      string `json:"agent"`
	Status     string `json:"status"`
	Calls      int32  `json:"calls"`
	CreatedAt  string `json:"created_at"`
	FinishedAt string `json:"finished_at"`
	Unresolved bool   `json:"unresolved"`
}

type RunHistory struct {
	Runs      []RunSummary `json:"runs"`
	Truncated bool         `json:"truncated"`
}

type RunHistoryHandler struct {
	pool    *pgxpool.Pool
	resolve func(*http.Request) (erp.Principal, error)
}

// NewRunHistoryHandler exposes the same System Manager/company boundary as
// existing agent events. It has no enqueue, retry or reconciliation capability.
func NewRunHistoryHandler(pool *pgxpool.Pool, resolve func(*http.Request) (erp.Principal, error)) *RunHistoryHandler {
	return &RunHistoryHandler{pool: pool, resolve: resolve}
}

func (h *RunHistoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "read only", http.StatusMethodNotAllowed)
		return
	}
	if h.pool == nil || h.resolve == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	p, err := h.resolve(r)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, erp.ErrUnauthenticated) {
			status = http.StatusUnauthorized
		}
		if errors.Is(err, erp.ErrForbidden) {
			status = http.StatusForbidden
		}
		http.Error(w, "authentication required", status)
		return
	}
	query := r.URL.Query()
	company := query.Get("company")
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || len(query) != 1 || len(query["company"]) != 1 || !validName(company) || p.ID == "" || !slices.Contains(p.Roles, "System Manager") || !slices.Contains(p.Companies, company) {
		http.Error(w, "request denied", http.StatusForbidden)
		return
	}
	rows, err := sqlc.New(h.pool).ListAgentRunMetadata(ctx, company)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	out := RunHistory{Runs: []RunSummary{}, Truncated: len(rows) > 25}
	if out.Truncated {
		rows = rows[:25]
	}
	for _, row := range rows {
		if !validID(row.ID) || !validID(row.AgentKey) || !slices.Contains([]string{"queued", "running", "completed", "needs_human", "failed", "cancelled"}, row.Status) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		finished := ""
		if row.FinishedAt.Valid {
			finished = row.FinishedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		out.Runs = append(out.Runs, RunSummary{ID: row.ID, Agent: row.AgentKey, Status: row.Status, Calls: row.CallCount, CreatedAt: row.CreatedAt.Time.UTC().Format(time.RFC3339Nano), FinishedAt: finished, Unresolved: row.Unresolved})
	}
	_ = json.NewEncoder(w).Encode(out)
}
