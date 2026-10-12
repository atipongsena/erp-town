package agents

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeProvidersTranslateToolsUsageAndHistory(t *testing.T) {
	for _, kind := range []string{"anthropic", "openai"} {
		t.Run(kind, func(t *testing.T) {
			var count atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := count.Add(1)
				raw, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				assert.NotContains(t, string(raw), "RUN-1")
				assert.NotContains(t, string(raw), "agent@test")
				var body map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(raw, &body))
				assert.Contains(t, body, "tools")
				w.Header().Set("Content-Type", "application/json")
				if kind == "anthropic" {
					assert.Equal(t, "/v1/messages", r.URL.Path)
					assert.Equal(t, "local-fixture", r.Header.Get("X-Api-Key"))
					assert.Empty(t, r.Header.Get("Authorization"))
					assert.JSONEq(t, `100`, string(body["max_tokens"]))
					if n == 1 {
						_, _ = io.WriteString(w, `{"id":"msg-1","type":"message","role":"assistant","content":[{"type":"tool_use","id":"call-1","name":"read_order","input":{"name":"SO-1"}}],"model":"fixture","stop_reason":"tool_use","usage":{"input_tokens":2,"output_tokens":3}}`)
					} else {
						assert.Contains(t, string(raw), "tool_result")
						_, _ = io.WriteString(w, `{"id":"msg-2","type":"message","role":"assistant","content":[{"type":"text","text":"Ready for review."}],"model":"fixture","stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":5}}`)
					}
				} else {
					assert.Equal(t, "/responses", r.URL.Path)
					assert.JSONEq(t, `false`, string(body["store"]))
					assert.JSONEq(t, `false`, string(body["parallel_tool_calls"]))
					assert.JSONEq(t, `100`, string(body["max_output_tokens"]))
					if n == 1 {
						_, _ = io.WriteString(w, `{"id":"resp-1","object":"response","status":"completed","output":[{"type":"function_call","call_id":"call-1","name":"read_order","arguments":"{\"name\":\"SO-1\"}"}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`)
					} else {
						assert.Contains(t, string(raw), "function_call_output")
						assert.NotContains(t, string(raw), "previous_response_id")
						_, _ = io.WriteString(w, `{"id":"resp-2","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Ready for review."}]}],"usage":{"input_tokens":4,"output_tokens":5,"total_tokens":9}}`)
					}
				}
			}))
			defer server.Close()
			p, err := NewLocalProvider(kind, LocalProviderConfig{Model: "fixture", BaseURL: server.URL})
			require.NoError(t, err)
			req := Request{RunID: "RUN-1", Instruction: "Read the order.", Tools: spec().Tools, RemainingTokens: 100}
			first, err := p.Step(t.Context(), req)
			require.NoError(t, err)
			assert.Equal(t, Usage{2, 3}, first.Usage)
			require.Len(t, first.Calls, 1)
			assert.Equal(t, "call-1", first.Calls[0].ID)
			req.History = []Turn{{Calls: first.Calls, Results: []CallResult{{ID: "call-1", Output: json.RawMessage(`{"status":"Draft"}`)}}}}
			second, err := p.Step(t.Context(), req)
			require.NoError(t, err)
			assert.Equal(t, "Ready for review.", second.Text)
			assert.Equal(t, Usage{4, 5}, second.Usage)
			assert.Equal(t, int64(2), count.Load())
		})
	}
}

func TestNativeProvidersDoNotRetryAndRedactTransportErrors(t *testing.T) {
	for _, kind := range []string{"anthropic", "openai"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(429)
				_, _ = io.WriteString(w, `{"error":{"message":"private prompt secret"}}`)
			}))
			defer server.Close()
			p, err := NewLocalProvider(kind, LocalProviderConfig{Model: "fixture", BaseURL: server.URL})
			require.NoError(t, err)
			_, err = p.Step(t.Context(), Request{Instruction: "Read.", Tools: spec().Tools, RemainingTokens: 10})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secret")
			assert.Equal(t, int64(1), calls.Load())
		})
	}
}

func TestNativeProvidersRejectExternalEndpoints(t *testing.T) {
	// Installed Codex app-server cannot yet prove the approved tool-only boundary.
	// An otherwise valid local endpoint must not enable it or a fallback provider.
	p, err := NewLocalProvider("codex", LocalProviderConfig{Model: "fixture", BaseURL: "http://127.0.0.1:1234"})
	require.ErrorIs(t, err, errProviderDisabled)
	require.Nil(t, p)
	for _, url := range []string{"", "https://api.openai.com", "http://localhost:1234", "http://127.0.0.1.evil.test", "http://user:pass@127.0.0.1:1234"} {
		_, err := NewLocalProvider("openai", LocalProviderConfig{Model: "fixture", BaseURL: url})
		require.Error(t, err)
	}
}

func TestNativeProvidersRejectMalformedAndOversizedReplies(t *testing.T) {
	for _, kind := range []string{"anthropic", "openai"} {
		for _, reply := range []string{`{}`, `{"usage":{},"output":[{"type":"local_shell_call"}]}`, strings.Repeat("x", 300000)} {
			t.Run(kind, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, reply)
				}))
				defer server.Close()
				p, err := NewLocalProvider(kind, LocalProviderConfig{Model: "fixture", BaseURL: server.URL})
				require.NoError(t, err)
				_, err = p.Step(t.Context(), Request{Instruction: "Read.", Tools: spec().Tools, RemainingTokens: 10})
				require.Error(t, err)
			})
		}
	}
}

func TestNativeProvidersHonorCancellation(t *testing.T) {
	for _, kind := range []string{"anthropic", "openai"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
				<-r.Context().Done()
			}))
			defer server.Close()
			p, err := NewLocalProvider(kind, LocalProviderConfig{Model: "fixture", BaseURL: server.URL})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			_, err = p.Step(ctx, Request{Instruction: "Read.", Tools: spec().Tools, RemainingTokens: 10})
			require.Error(t, err)
		})
	}
}
