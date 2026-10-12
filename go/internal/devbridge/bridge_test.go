package devbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atipongsena/erp-town/go/internal/store/storetest"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m, storetest.IgnoreBackgroundGoroutines()...) }

const testToken = "local-test-token-with-at-least-32-bytes"

func TestLoopbackHTTPAcceptsSafeEventsAndListenerShutsDown(t *testing.T) {
	b, err := New(storetest.NewPool(t), testToken, map[string]Mapping{"codex": {Company: "ACME", Actor: "agent.dev_codex"}})
	require.NoError(t, err)
	server := httptest.NewServer(b)
	defer server.Close()
	for _, accepted := range []bool{true, false} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/dev/task", strings.NewReader(`{"provider":"codex","task_id":"HTTP-1","sequence":1,"state":"started"}`))
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		require.Equal(t, 200, response.StatusCode)
		var result map[string]bool
		err = json.NewDecoder(response.Body).Decode(&result)
		require.NoError(t, response.Body.Close())
		require.NoError(t, err)
		require.Equal(t, map[string]bool{"accepted": accepted}, result)
	}
	require.ErrorIs(t, Serve(t.Context(), "0.0.0.0:0", b), ErrRejected)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, "127.0.0.1:0", b) }()
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("loopback listener failed to shut down")
	}
}

func TestConcurrentDuplicateHooksCommitOneOwnedEvent(t *testing.T) {
	pool := storetest.NewPool(t)
	b, err := New(pool, testToken, map[string]Mapping{"claude": {Company: "ACME", Actor: "agent.dev_claude"}})
	require.NoError(t, err)
	var accepted atomic.Int64
	failures := make(chan error, 12)
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() {
			ok, err := b.Accept(t.Context(), Task{Provider: "claude", TaskID: "CONCURRENT", Sequence: 1, State: "started"})
			if err != nil {
				failures <- err
			}
			if ok {
				accepted.Add(1)
			}
		})
	}
	group.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), accepted.Load())
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM town_outbox").Scan(&n))
	require.Equal(t, 1, n)
	var seq int64
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT last_seq FROM town_subject_seq WHERE subject_doctype='Town Dev Task' AND subject_name='claude/CONCURRENT'").Scan(&seq))
	require.Equal(t, int64(1), seq)
}

func TestBridgeOrderedTerminalRestartAndPrivacy(t *testing.T) {
	pool := storetest.NewPool(t)
	b, err := New(pool, testToken, map[string]Mapping{"codex": {Company: "ACME", Actor: "agent.dev_codex"}})
	require.NoError(t, err)
	for _, event := range []Task{{Provider: "codex", TaskID: "TASK-1", Sequence: 1, State: "started"}, {Provider: "codex", TaskID: "TASK-1", Sequence: 2, State: "running"}, {Provider: "codex", TaskID: "TASK-1", Sequence: 3, State: "completed"}} {
		accepted, err := b.Accept(t.Context(), event)
		require.NoError(t, err)
		require.True(t, accepted)
	}
	b, err = New(pool, testToken, map[string]Mapping{"codex": {Company: "ACME", Actor: "agent.dev_codex"}})
	require.NoError(t, err)
	accepted, err := b.Accept(t.Context(), Task{Provider: "codex", TaskID: "TASK-1", Sequence: 3, State: "completed"})
	require.NoError(t, err)
	require.False(t, accepted)
	accepted, err = b.Accept(t.Context(), Task{Provider: "codex", TaskID: "TASK-1", Sequence: 4, State: "running"})
	require.NoError(t, err)
	require.False(t, accepted)
	var n int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM town_outbox").Scan(&n))
	require.Equal(t, 3, n)
	var payload string
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT payload FROM town_outbox ORDER BY insert_order DESC LIMIT 1").Scan(&payload))
	var event map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &event))
	require.Equal(t, "ACME", event["company"])
	for _, secret := range []string{testToken, "\"prompt\":", "\"code\":", "\"cwd\":", "\"path\":", "on_behalf_of\":\"", "api_key"} {
		require.NotContains(t, payload, secret)
	}
}

func TestBridgeRejectsForgedScopeDuplicatesOriginsRemoteAndOversize(t *testing.T) {
	b, err := New(storetest.NewPool(t), testToken, map[string]Mapping{"codex": {Company: "ACME", Actor: "agent.dev_codex"}})
	require.NoError(t, err)
	good := `{"provider":"codex","task_id":"TASK-1","sequence":1,"state":"started"}`
	for _, tc := range []struct {
		body, remote, origin, token string
		status                      int
	}{
		{good, "192.0.2.1:1234", "", testToken, 403},
		{good, "127.0.0.1:1234", "http://localhost", testToken, 403},
		{good, "127.0.0.1:1234", "", "wrong", 401},
		{strings.TrimSuffix(good, "}") + `,"company":"OTHER"}`, "127.0.0.1:1234", "", testToken, 400},
		{strings.TrimSuffix(good, "}") + `,"state":"failed"}`, "127.0.0.1:1234", "", testToken, 400},
		{strings.Repeat("x", 4097), "127.0.0.1:1234", "", testToken, 400},
	} {
		r := httptest.NewRequest(http.MethodPost, "/dev/task", strings.NewReader(tc.body))
		r.RemoteAddr = tc.remote
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("Content-Type", "application/json")
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		require.Equal(t, tc.status, w.Code)
	}
}
