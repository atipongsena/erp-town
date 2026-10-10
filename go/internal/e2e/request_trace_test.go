package e2e

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp/erpnext"
	"github.com/atipongsena/erp-town/go/internal/gateway"
	"github.com/atipongsena/erp-town/go/internal/platform"
)

func TestGatewayRequestIDReachesERPNextOnEveryAttempt(t *testing.T) {
	var mu sync.Mutex
	var ids, paths []string
	orderAttempts := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		ids = append(ids, r.Header.Get("X-Request-Id"))
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/method/erp_town.session_api.whoami":
			_, _ = w.Write([]byte(`{"message":{"user":"sales@acme-th.test","roles":["Sales User"],"companies":["ACME-TH"],"csrf_token":"csrf-secret"}}`))
		case "/api/resource/Sales Order/SO6910-0001":
			orderAttempts++
			if orderAttempts == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"name":"SO6910-0001","docstatus":0,"company":"Example Company","customer":"CUST-001","items":[]}}`))
		case "/api/resource/Company":
			_, _ = w.Write([]byte(`{"data":[{"name":"Example Company","abbr":"ACME-TH"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(backend.Close)
	adapter := erpnext.NewAdapter(erpnext.NewClient(erpnext.ClientConfig{BaseURL: backend.URL, Transport: backend.Client().Transport}))
	issuer, err := auth.NewJWTIssuer(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)), 15*time.Minute)
	require.NoError(t, err)
	mux := platform.NewMux()
	gateway.New(mux, adapter, auth.NewAuthenticator(adapter, 30*time.Second), issuer)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/town/api/sales-orders/SO6910-0001", nil)
	r.AddCookie(&http.Cookie{Name: "sid", Value: "session-secret"})
	r.Header.Set("X-Request-Id", "untrusted-client-id")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	id := w.Header().Get("X-Request-Id")
	require.NotEmpty(t, id)
	assert.NotEqual(t, "untrusted-client-id", id)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, ids, 4, "whoami, GET and retry, company lookup")
	for i, got := range ids {
		assert.Equal(t, id, got, "backend call %s", paths[i])
	}
}
