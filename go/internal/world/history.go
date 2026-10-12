package world

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/redis/go-redis/v9"
)

var ErrHistoryDenied = errors.New("world: history denied")

type (
	HistorySource interface {
		Read(context.Context, time.Time, time.Time, int) ([]events.Envelope, bool, error)
	}
	Transition struct {
		Time    string `json:"time"`
		DocType string `json:"doctype"`
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Status  string `json:"status"`
	}
	HistoryView struct {
		Transitions []Transition `json:"transitions"`
		Truncated   bool         `json:"truncated"`
	}
	History struct {
		source HistorySource
		now    func() time.Time
	}
)

func NewHistory(s HistorySource) *History { return &History{source: s, now: time.Now} }

// Read provides a bounded list of coarse transitions from retained events, not
// historical ERP documents or a guaranteed reconstruction of earlier balances.
func (h *History) Read(ctx context.Context, viewer auth.Claims, company string, from, until time.Time, limit int) (HistoryView, error) {
	if h.source == nil || viewer.Subject == "" || !h.now().Before(viewer.ExpiresAt) || !slices.Contains(viewer.Companies, company) || limit < 1 || limit > 1000 || !from.Before(until) || until.Sub(from) > 24*time.Hour || until.After(h.now().Add(time.Second)) {
		return HistoryView{}, ErrHistoryDenied
	}
	list, truncated, err := h.source.Read(ctx, from, until, 1001)
	if err != nil {
		return HistoryView{}, err
	}
	if len(list) > 1000 {
		list = list[:1000]
		truncated = true
	}
	v, err := events.NewValidator()
	if err != nil {
		return HistoryView{}, err
	}
	out := HistoryView{Transitions: []Transition{}, Truncated: truncated}
	for _, e := range list {
		if e.Company == nil || *e.Company != company || !strings.HasPrefix(e.Type, "erp.") && !strings.HasPrefix(e.Type, "workflow.") && !strings.HasPrefix(e.Type, "agent.run.") {
			continue
		}
		when, err := time.Parse(time.RFC3339Nano, e.Time)
		if err != nil || when.Before(from) || when.After(until) {
			continue
		}
		raw, err := events.Marshal(e)
		if err != nil || v.Validate(raw) != nil {
			continue
		}
		key, err := entityKey(e)
		if err != nil || !visibleTo(Entity{Actor: e.Actor, Visibility: e.Visibility}, viewer) {
			continue
		}
		status := "updated"
		if key.Kind == KindDocument {
			if strings.HasSuffix(e.Type, ".submitted") {
				status = "submitted"
			} else if strings.HasSuffix(e.Type, ".cancelled") {
				status = "cancelled"
			}
		}
		if key.Kind == KindApproval {
			status = "approval"
			if strings.HasSuffix(e.Type, ".decided") {
				status = "decided"
			}
		}
		if key.Kind == KindOverdue {
			status = "overdue"
		}
		if key.Kind == KindStock {
			status = "stock"
		}
		if key.Kind == KindAgentRun {
			var data struct {
				State string `json:"state"`
			}
			if json.Unmarshal(e.Data, &data) != nil {
				continue
			}
			status = "ai-" + data.State
		}
		if len(out.Transitions) >= limit {
			out.Truncated = true
			break
		}
		out.Transitions = append(out.Transitions, Transition{Time: e.Time, DocType: e.Subject.DocType, Name: e.Subject.Name, Kind: string(key.Kind), Status: status})
	}
	return out, nil
}

type RedisHistory struct {
	client *redis.Client
	stream string
}

func NewRedisHistory(c *redis.Client, stream string) *RedisHistory {
	return &RedisHistory{client: c, stream: stream}
}

func (s *RedisHistory) Read(ctx context.Context, from, until time.Time, limit int) ([]events.Envelope, bool, error) {
	if limit < 1 || limit > 1001 {
		return nil, false, ErrHistoryDenied
	}
	start := strconv.FormatInt(from.UnixMilli(), 10) + "-0"
	end := strconv.FormatInt(until.UnixMilli(), 10) + "-18446744073709551615"
	rows, err := s.client.XRangeN(ctx, s.stream, start, end, int64(limit)).Result()
	if err != nil {
		return nil, false, errors.New("world: retained history unavailable")
	}
	first, err := s.client.XRangeN(ctx, s.stream, "-", "+", 1).Result()
	if err != nil {
		return nil, false, errors.New("world: retained history unavailable")
	}
	truncated := len(rows) == limit
	if len(first) > 0 && maxStreamID(start, first[0].ID) == first[0].ID && start != first[0].ID {
		truncated = true
	}
	v, err := events.NewValidator()
	if err != nil {
		return nil, false, err
	}
	out := []events.Envelope{}
	for _, row := range rows {
		raw, ok := row.Values["event"].(string)
		if !ok {
			continue
		}
		e, err := events.Decode(v, []byte(raw))
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, truncated, nil
}

type HistoryResolver func(*http.Request) (auth.Claims, error)

// NewHistoryHandler resolves fresh membership at query time; it never trusts a
// viewer from the query/body and offers no export or document-read operation.
func NewHistoryHandler(h *History, resolve HistoryResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		if resolve == nil || h == nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		viewer, err := resolve(r)
		if err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		from, e1 := time.Parse(time.RFC3339Nano, q.Get("from"))
		until, e2 := time.Parse(time.RFC3339Nano, q.Get("until"))
		limit, e3 := strconv.Atoi(q.Get("limit"))
		if e1 != nil || e2 != nil || e3 != nil {
			http.Error(w, "invalid range", http.StatusBadRequest)
			return
		}
		out, err := h.Read(ctx, viewer, q.Get("company"), from, until, limit)
		if err != nil {
			http.Error(w, "history unavailable", http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}
