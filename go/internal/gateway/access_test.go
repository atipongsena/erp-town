package gateway_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp/erpfake"
)

func TestGatewayAccessLogCorrelatesEveryOutcome(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	h := newHarness(t, erpfake.DefaultEnv().Port)
	for _, tc := range []struct {
		name, method, path, body string
		opts                     []reqOpt
		status                   int
	}{
		{"success", http.MethodGet, "/town/api/me", "", []reqOpt{withSession(salesSession)}, 200},
		{"unauthenticated", http.MethodGet, "/town/api/me", "", nil, 401},
		{"csrf", http.MethodPost, "/town/api/sales-orders", orderBody, nil, 403},
		{"validation", http.MethodPost, "/town/api/sales-orders", "{", []reqOpt{withSession(salesSession), withTownHeader()}, 422},
		{"unknown", http.MethodGet, "/town/api/no-such-route?token=private-query", "", nil, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs.Reset()
			w := h.do(tc.method, tc.path, tc.body, tc.opts...)
			require.Equal(t, tc.status, w.Code)
			var entry map[string]any
			require.NoError(t, json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &entry))
			assert.Equal(t, w.Header().Get("X-Request-Id"), entry["request_id"])
			assert.NotEmpty(t, entry["request_id"])
			assert.Equal(t, tc.method, entry["method"])
			assert.Equal(t, float64(tc.status), entry["status"])
			assert.Contains(t, entry, "duration_ms")
			assert.Contains(t, entry, "path")
			assert.NotContains(t, logs.String(), "private-query")
			assert.NotContains(t, logs.String(), salesSession)
			assert.NotContains(t, logs.String(), "CUST-001")
		})
	}
}
