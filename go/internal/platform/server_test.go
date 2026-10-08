package platform

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	NewMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/town/api/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}

func TestServeShutsDownOnCancel(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, addr, NewMux()) }()

	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/town/api/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 20*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("Serve did not return")
	}
	http.DefaultClient.CloseIdleConnections()
}
