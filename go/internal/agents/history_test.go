package agents

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
	"github.com/stretchr/testify/require"
)

func TestRunHistoryIsBoundedCompanyScopedAndContainsNoPrivateJournal(t *testing.T) {
	pool := storetest.NewPool(t)
	for i := range 27 {
		_, err := pool.Exec(t.Context(), "INSERT INTO agent_runs(id,agent_key,company,delegator,status,reason) VALUES($1,'helper','ACME','private-human','completed','private-provider-error')", fmt.Sprintf("run-%02d", i))
		require.NoError(t, err)
	}
	_, err := pool.Exec(t.Context(), "INSERT INTO agent_runs(id,agent_key,company,delegator,status) VALUES('other-run','helper','OTHER','private-human','queued'),('uncertain-run','helper','ACME','private-human','needs_human')")
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), "INSERT INTO agent_tool_calls(run_id,call_id,tool,intent_hash,is_write,status) VALUES('uncertain-run','call-1','create_sales_order',repeat('a',64),true,'uncertain')")
	require.NoError(t, err)
	p := erp.Principal{ID: "manager", Roles: []string{"System Manager"}, Companies: []string{"ACME"}}
	var resolveErr error
	h := NewRunHistoryHandler(pool, func(*http.Request) (erp.Principal, error) { return p, resolveErr })
	r := httptest.NewRequest(http.MethodGet, "/town/api/agent-runs?company=ACME", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var result RunHistory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Runs, 25)
	require.True(t, result.Truncated)
	require.Equal(t, "uncertain-run", result.Runs[0].ID)
	require.True(t, result.Runs[0].Unresolved)
	for _, private := range []string{"private-human", "private-provider-error", "intent_hash", "other-run", "create_sales_order"} {
		require.NotContains(t, w.Body.String(), private)
	}
	for _, query := range []string{"company=OTHER", "company=ACME&user=manager", "company=ACME&company=OTHER"} {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/town/api/agent-runs?"+query, nil))
		require.Equal(t, 403, w.Code)
	}
	p.Roles = []string{"Sales User"}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, 403, w.Code, "fresh resolver revocation takes effect on the next request")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/town/api/agent-runs?company=ACME", nil))
	require.Equal(t, 405, w.Code, "history must never retry or reconcile a write")
	for _, denied := range []struct {
		err    error
		status int
	}{{erp.ErrUnauthenticated, 401}, {erp.ErrUnavailable, 503}} {
		resolveErr = denied.err
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, denied.status, w.Code)
	}
}
