package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/atipongsena/erp-town/go/internal/store/storetest"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m, storetest.IgnoreBackgroundGoroutines()...) }

type providerFunc func(context.Context, Request) (Response, error)

func (f providerFunc) Step(ctx context.Context, req Request) (Response, error) { return f(ctx, req) }

type executorFunc func(context.Context, Call) (ToolResult, error)

func (f executorFunc) Execute(ctx context.Context, call Call) (ToolResult, error) {
	return f(ctx, call)
}

type journal struct {
	entries   []AuditEntry
	failPhase string
}

func (j *journal) Record(_ context.Context, entry AuditEntry) error {
	if entry.Phase == j.failPhase {
		return errors.New("private connection details")
	}
	j.entries = append(j.entries, entry)
	return nil
}

func spec() Spec {
	return Spec{ID: "RUN-1", Instruction: "Read the task, then prepare a draft.", Tools: []ToolDefinition{
		{Name: "read_order", Description: "Read one order.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}}
}

func call(id string) Call {
	return Call{ID: id, Tool: "read_order", Arguments: json.RawMessage(`{"name":"SO-1"}`)}
}

func step(calls ...Call) Response {
	return Response{Calls: calls, Usage: Usage{InputTokens: 3, OutputTokens: 2}}
}

func finished() Response {
	return Response{Text: "The draft is ready for review.", Usage: Usage{InputTokens: 3, OutputTokens: 2}}
}

func execute(t *testing.T, p Provider, x Executor, a Audit, limits Limits) Result {
	t.Helper()
	r, err := NewRun(spec(), p, x, a, limits)
	require.NoError(t, err)
	result, err := r.Execute(t.Context())
	require.NoError(t, err)
	return result
}

func TestRunFeedsToolResultsBackToProvider(t *testing.T) {
	var requests []Request
	p := providerFunc(func(_ context.Context, req Request) (Response, error) {
		requests = append(requests, req)
		if len(requests) == 1 {
			return step(call("call-1")), nil
		}
		return finished(), nil
	})
	var calls []Call
	x := executorFunc(func(_ context.Context, c Call) (ToolResult, error) {
		calls = append(calls, c)
		return ToolResult{Output: json.RawMessage(`{"status":"Draft","amount":"9007199254740993.01"}`)}, nil
	})
	a := &journal{}
	result := execute(t, p, x, a, DefaultLimits())
	assert.Equal(t, Completed, result.Status)
	assert.Equal(t, finished().Text, result.Text)
	assert.Equal(t, 2, result.Turns)
	assert.Equal(t, 1, result.ToolCalls)
	assert.Equal(t, int64(10), result.TokensUsed)
	require.Len(t, requests, 2)
	require.Len(t, calls, 1)
	require.Len(t, requests[1].History, 1)
	assert.JSONEq(t, `{"status":"Draft","amount":"9007199254740993.01"}`, string(requests[1].History[0].Results[0].Output))
	assert.Equal(t, int64(11995), requests[1].RemainingTokens)
	assert.Equal(t, []AuditEntry{{"RUN-1", "call-1", "read_order", "started"}, {"RUN-1", "call-1", "read_order", "completed"}}, a.entries)
}

func TestRunReplayedCallDoesNotRepeatTool(t *testing.T) {
	turn, count := 0, 0
	p := providerFunc(func(_ context.Context, req Request) (Response, error) {
		turn++
		if turn <= 2 {
			return step(call("same-call")), nil
		}
		require.Len(t, req.History, 2)
		assert.Equal(t, req.History[0].Results, req.History[1].Results)
		return finished(), nil
	})
	x := executorFunc(func(_ context.Context, _ Call) (ToolResult, error) {
		count++
		return ToolResult{Output: json.RawMessage(`{"draft":"SO-1"}`)}, nil
	})
	a := &journal{}
	result := execute(t, p, x, a, DefaultLimits())
	assert.Equal(t, Completed, result.Status)
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, result.ToolCalls)
	assert.Len(t, a.entries, 2)
}

func TestRunValidatesWholeBatchBeforeAnyTool(t *testing.T) {
	for _, tc := range []struct {
		name   string
		calls  []Call
		reason Reason
	}{
		{"unknown later tool", []Call{call("ok"), {ID: "bad", Tool: "submit_payment", Arguments: json.RawMessage(`{}`)}}, UnknownTool},
		{"conflicting ids", []Call{call("same"), {ID: "same", Tool: "read_order", Arguments: json.RawMessage(`{"name":"SO-2"}`)}}, CallConflict},
		{"invalid later json", []Call{call("ok"), {ID: "bad", Tool: "read_order", Arguments: json.RawMessage(`null`)}}, InvalidResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			p := providerFunc(func(context.Context, Request) (Response, error) { return step(tc.calls...), nil })
			x := executorFunc(func(context.Context, Call) (ToolResult, error) {
				count++
				return ToolResult{Output: json.RawMessage(`{}`)}, nil
			})
			a := &journal{}
			result := execute(t, p, x, a, DefaultLimits())
			assert.Equal(t, NeedsHuman, result.Status)
			assert.Equal(t, tc.reason, result.Reason)
			assert.Zero(t, count)
			assert.Empty(t, a.entries)
		})
	}
}

func TestRunConflictingReplayStopsAfterFirstTool(t *testing.T) {
	turn, count := 0, 0
	p := providerFunc(func(context.Context, Request) (Response, error) {
		turn++
		c := call("same")
		if turn > 1 {
			c.Arguments = json.RawMessage(`{"name":"SO-2"}`)
		}
		return step(c), nil
	})
	x := executorFunc(func(context.Context, Call) (ToolResult, error) {
		count++
		return ToolResult{Output: json.RawMessage(`{}`)}, nil
	})
	result := execute(t, p, x, &journal{}, DefaultLimits())
	assert.Equal(t, NeedsHuman, result.Status)
	assert.Equal(t, CallConflict, result.Reason)
	assert.Equal(t, 1, count)
}

func TestRunStopsOnUncertainToolOrHumanReview(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output ToolResult
		err    error
		reason Reason
		phase  string
	}{
		{"lost write acknowledgment", ToolResult{}, errors.New("private ERP body"), ToolUncertain, "uncertain"},
		{"pending human approval", ToolResult{Output: json.RawMessage(`{"draft":"SO-1"}`), NeedsHuman: true}, nil, HumanRequired, "needs_human"},
		{"malformed tool reply", ToolResult{Output: json.RawMessage(`not JSON`)}, nil, ToolUncertain, "uncertain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			turn, count := 0, 0
			p := providerFunc(func(context.Context, Request) (Response, error) {
				turn++
				return step(call("first"), call("second")), nil
			})
			x := executorFunc(func(context.Context, Call) (ToolResult, error) { count++; return tc.output, tc.err })
			a := &journal{}
			result := execute(t, p, x, a, DefaultLimits())
			assert.Equal(t, NeedsHuman, result.Status)
			assert.Equal(t, tc.reason, result.Reason)
			assert.Equal(t, 1, count)
			assert.Equal(t, 1, turn)
			assert.Equal(t, []AuditEntry{{"RUN-1", "first", "read_order", "started"}, {"RUN-1", "first", "read_order", tc.phase}}, a.entries)
			b, err := json.Marshal(result)
			require.NoError(t, err)
			assert.NotContains(t, string(b), "private")
		})
	}
}

func TestRunAuditFailureStopsDispatch(t *testing.T) {
	for _, phase := range []string{"started", "completed"} {
		t.Run(phase, func(t *testing.T) {
			count, turn := 0, 0
			p := providerFunc(func(context.Context, Request) (Response, error) {
				turn++
				return step(call("first"), call("second")), nil
			})
			x := executorFunc(func(context.Context, Call) (ToolResult, error) {
				count++
				return ToolResult{Output: json.RawMessage(`{}`)}, nil
			})
			result := execute(t, p, x, &journal{failPhase: phase}, DefaultLimits())
			assert.Equal(t, NeedsHuman, result.Status)
			assert.Equal(t, AuditUnavailable, result.Reason)
			if phase == "started" {
				assert.Zero(t, count)
			} else {
				assert.Equal(t, 1, count)
			}
			assert.Equal(t, 1, turn)
		})
	}
}

func TestRunBoundsToolsTurnsAndTokens(t *testing.T) {
	for _, tc := range []struct {
		name     string
		change   func(*Limits)
		reason   Reason
		maxTools int
	}{
		{"turns", func(l *Limits) { l.MaxTurns = 1 }, TurnLimit, 1},
		{"tools", func(l *Limits) { l.MaxToolCalls = 1 }, ToolLimit, 0},
		{"tokens", func(l *Limits) { l.MaxTokens = 4 }, TokenLimit, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := DefaultLimits()
			tc.change(&limits)
			count, turn := 0, 0
			p := providerFunc(func(context.Context, Request) (Response, error) {
				turn++
				if tc.reason == ToolLimit {
					return step(call("one"), call("two")), nil
				}
				return step(call("one")), nil
			})
			x := executorFunc(func(context.Context, Call) (ToolResult, error) {
				count++
				return ToolResult{Output: json.RawMessage(`{}`)}, nil
			})
			result := execute(t, p, x, &journal{}, limits)
			assert.Equal(t, NeedsHuman, result.Status)
			assert.Equal(t, tc.reason, result.Reason)
			assert.Equal(t, tc.maxTools, count)
			assert.Equal(t, 1, turn)
		})
	}
}

func TestRunCancellationAndDeadline(t *testing.T) {
	for _, cancelBeforeStart := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelBeforeStart), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelBeforeStart {
				cancel()
			}
			p := providerFunc(func(ctx context.Context, _ Request) (Response, error) { <-ctx.Done(); return Response{}, ctx.Err() })
			x := executorFunc(func(context.Context, Call) (ToolResult, error) {
				t.Fatal("cancelled run must not call tools")
				return ToolResult{}, nil
			})
			limits := DefaultLimits()
			limits.Timeout = 20 * time.Millisecond
			r, err := NewRun(spec(), p, x, &journal{}, limits)
			require.NoError(t, err)
			result, err := r.Execute(ctx)
			require.NoError(t, err)
			assert.Equal(t, Cancelled, result.Status)
			_, err = r.Execute(t.Context())
			require.ErrorIs(t, err, ErrAlreadyExecuted)
		})
	}
}

func TestRunCannotExecuteConcurrentlyTwice(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	p := providerFunc(func(context.Context, Request) (Response, error) { close(entered); <-release; return finished(), nil })
	x := executorFunc(func(context.Context, Call) (ToolResult, error) { return ToolResult{}, nil })
	r, err := NewRun(spec(), p, x, &journal{}, DefaultLimits())
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Go(func() {
		result, err := r.Execute(t.Context())
		assert.NoError(t, err)
		assert.Equal(t, Completed, result.Status)
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		wg.Wait()
		t.Fatal("provider was not called")
	}
	_, err = r.Execute(t.Context())
	assert.ErrorIs(t, err, ErrAlreadyExecuted)
	close(release)
	wg.Wait()
}

type auditFunc func(context.Context, AuditEntry) error

func (f auditFunc) Record(ctx context.Context, entry AuditEntry) error { return f(ctx, entry) }

func TestRunAdapterPanicsAreContained(t *testing.T) {
	for _, target := range []string{"provider", "executor", "audit-start", "audit-end"} {
		t.Run(target, func(t *testing.T) {
			count := 0
			p := providerFunc(func(context.Context, Request) (Response, error) {
				if target == "provider" {
					panic("private prompt")
				}
				return step(call("one"), call("two")), nil
			})
			x := executorFunc(func(context.Context, Call) (ToolResult, error) {
				count++
				if target == "executor" {
					panic(nil)
				}
				return ToolResult{Output: json.RawMessage(`{}`)}, nil
			})
			a := auditFunc(func(_ context.Context, entry AuditEntry) error {
				if target == "audit-start" && entry.Phase == "started" || target == "audit-end" && entry.Phase == "completed" {
					panic("private storage details")
				}
				return nil
			})
			var result Result
			require.NotPanics(t, func() { result = execute(t, p, x, a, DefaultLimits()) })
			if target == "provider" {
				assert.Equal(t, Failed, result.Status)
				assert.Equal(t, ProviderUnavailable, result.Reason)
				assert.Zero(t, count)
			} else {
				assert.Equal(t, NeedsHuman, result.Status)
				if target == "executor" {
					assert.Equal(t, ToolUncertain, result.Reason)
				} else {
					assert.Equal(t, AuditUnavailable, result.Reason)
				}
				if target == "audit-start" {
					assert.Zero(t, count)
				} else {
					assert.Equal(t, 1, count)
				}
			}
		})
	}
}

func TestRunRejectsInvalidUsageAndResponses(t *testing.T) {
	for _, response := range []Response{
		{Text: "answer", Usage: Usage{InputTokens: -1}},
		{Text: "answer", Usage: Usage{InputTokens: math.MaxInt64, OutputTokens: 1}},
		{Usage: Usage{InputTokens: 1}},
		step(Call{ID: "bad\nid", Tool: "read_order", Arguments: json.RawMessage(`{}`)}),
	} {
		p := providerFunc(func(context.Context, Request) (Response, error) { return response, nil })
		x := executorFunc(func(context.Context, Call) (ToolResult, error) {
			t.Fatal("invalid response must not reach tools")
			return ToolResult{}, nil
		})
		result := execute(t, p, x, &journal{}, DefaultLimits())
		assert.Equal(t, NeedsHuman, result.Status)
		assert.Equal(t, InvalidResponse, result.Reason)
	}
}

func TestRunBoundsResponseToolOutputAndHistory(t *testing.T) {
	for _, target := range []string{"response", "tool", "history"} {
		t.Run(target, func(t *testing.T) {
			limits := DefaultLimits()
			limits.MaxPayloadBytes = 1024
			turn, count := 0, 0
			p := providerFunc(func(context.Context, Request) (Response, error) {
				turn++
				if target == "response" {
					return Response{Text: strings.Repeat("x", 1024)}, nil
				}
				return step(call("one")), nil
			})
			x := executorFunc(func(context.Context, Call) (ToolResult, error) {
				count++
				length := 800
				if target == "tool" {
					length = 1024
				}
				b, err := json.Marshal(strings.Repeat("x", length))
				require.NoError(t, err)
				return ToolResult{Output: b}, nil
			})
			result := execute(t, p, x, &journal{}, limits)
			assert.Equal(t, NeedsHuman, result.Status)
			if target == "tool" {
				assert.Equal(t, ToolUncertain, result.Reason)
			} else {
				assert.Equal(t, PayloadLimit, result.Reason)
			}
			assert.Equal(t, 1, turn)
			if target == "response" {
				assert.Zero(t, count)
			} else {
				assert.Equal(t, 1, count)
			}
		})
	}
}

func TestRunCopiesConfigurationAndAdapterInputs(t *testing.T) {
	input := spec()
	turn, count := 0, 0
	p := providerFunc(func(_ context.Context, req Request) (Response, error) {
		turn++
		assert.Equal(t, "read_order", req.Tools[0].Name)
		assert.JSONEq(t, `{"type":"object"}`, string(req.Tools[0].InputSchema))
		if turn == 3 {
			require.Len(t, req.History, 2)
			assert.JSONEq(t, `{"name":"SO-1"}`, string(req.History[0].Calls[0].Arguments))
			assert.JSONEq(t, `{"draft":"SO-1"}`, string(req.History[0].Results[0].Output))
			assert.Equal(t, req.History[0].Results, req.History[1].Results)
			return finished(), nil
		}
		req.Tools[0].Name = "submit_payment"
		req.Tools[0].InputSchema[0] = '!'
		if turn == 2 {
			req.History[0].Calls[0].Arguments[0] = '!'
			req.History[0].Results[0].Output[0] = '!'
		}
		return step(call("one")), nil
	})
	x := executorFunc(func(_ context.Context, c Call) (ToolResult, error) {
		count++
		c.Arguments[0] = '!'
		return ToolResult{Output: json.RawMessage(`{"draft":"SO-1"}`)}, nil
	})
	r, err := NewRun(input, p, x, &journal{}, DefaultLimits())
	require.NoError(t, err)
	input.Tools[0].Name = "submit_payment"
	input.Tools[0].InputSchema[0] = '!'
	result, err := r.Execute(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Completed, result.Status)
	assert.Equal(t, 1, count)
}

func TestRunCancellationDuringToolKeepsUncertainAudit(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p := providerFunc(func(context.Context, Request) (Response, error) { return step(call("one"), call("two")), nil })
	count := 0
	x := executorFunc(func(context.Context, Call) (ToolResult, error) {
		count++
		cancel()
		return ToolResult{}, context.Canceled
	})
	var entries []AuditEntry
	a := auditFunc(func(ctx context.Context, entry AuditEntry) error {
		assert.NoError(t, ctx.Err(), "outcome audit must outlive run cancellation")
		entries = append(entries, entry)
		return nil
	})
	r, err := NewRun(spec(), p, x, a, DefaultLimits())
	require.NoError(t, err)
	result, err := r.Execute(ctx)
	require.NoError(t, err)
	assert.Equal(t, NeedsHuman, result.Status)
	assert.Equal(t, ToolUncertain, result.Reason)
	assert.Equal(t, 1, count)
	assert.Equal(t, []AuditEntry{{"RUN-1", "one", "read_order", "started"}, {"RUN-1", "one", "read_order", "uncertain"}}, entries)
}

func TestRunDuplicateWithinBatchAndWhitespaceReplay(t *testing.T) {
	turn, count := 0, 0
	p := providerFunc(func(context.Context, Request) (Response, error) {
		turn++
		if turn == 1 {
			return step(call("one"), call("one")), nil
		}
		if turn == 2 {
			c := call("one")
			c.Arguments = json.RawMessage(` { "name" : "SO-1" } `)
			return step(c), nil
		}
		return finished(), nil
	})
	x := executorFunc(func(context.Context, Call) (ToolResult, error) {
		count++
		return ToolResult{Output: json.RawMessage(`{}`)}, nil
	})
	limits := DefaultLimits()
	limits.MaxToolCalls = 1
	result := execute(t, p, x, &journal{}, limits)
	assert.Equal(t, Completed, result.Status)
	assert.Equal(t, 1, count)
}

func TestNewRunRejectsUnsafeConfiguration(t *testing.T) {
	p := providerFunc(func(context.Context, Request) (Response, error) { return finished(), nil })
	x := executorFunc(func(context.Context, Call) (ToolResult, error) { return ToolResult{}, nil })
	for _, invalid := range []Spec{
		{},
		{ID: "RUN-1", Instruction: ""},
		{ID: "RUN-1", Instruction: strings.Repeat("x", 65536)},
		{ID: "RUN-1", Instruction: "task", Tools: []ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`null`)}}},
		{ID: "RUN-1", Instruction: "task", Tools: []ToolDefinition{{Name: "read", InputSchema: json.RawMessage(`{}`)}, {Name: "read", InputSchema: json.RawMessage(`{}`)}}},
	} {
		_, err := NewRun(invalid, p, x, &journal{}, DefaultLimits())
		require.ErrorIs(t, err, ErrInvalidRun)
	}
	_, err := NewRun(spec(), nil, x, &journal{}, DefaultLimits())
	require.ErrorIs(t, err, ErrInvalidRun)
	_, err = NewRun(spec(), p, x, nil, DefaultLimits())
	require.ErrorIs(t, err, ErrInvalidRun)
	_, err = NewRun(spec(), p, x, &journal{}, Limits{MaxTurns: -1})
	require.ErrorIs(t, err, ErrInvalidRun)
}

func TestRunProviderFailureDoesNotRetryEarlierTool(t *testing.T) {
	turn, count := 0, 0
	p := providerFunc(func(context.Context, Request) (Response, error) {
		turn++
		if turn == 1 {
			return step(call("draft")), nil
		}
		return Response{Text: "private model details"}, errors.New("private provider response")
	})
	x := executorFunc(func(context.Context, Call) (ToolResult, error) {
		count++
		return ToolResult{Output: json.RawMessage(`{"draft":"SO-1"}`)}, nil
	})
	result := execute(t, p, x, &journal{}, DefaultLimits())
	assert.Equal(t, Failed, result.Status)
	assert.Equal(t, ProviderUnavailable, result.Reason)
	assert.Equal(t, 1, count)
	assert.Equal(t, 2, turn)
	assert.Empty(t, result.Text)
}

func TestRunCancellationAroundToolDoesNotRepeatKnownEffect(t *testing.T) {
	for _, beforeTool := range []bool{false, true} {
		t.Run(fmt.Sprint(beforeTool), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			count := 0
			p := providerFunc(func(context.Context, Request) (Response, error) { return step(call("one"), call("two")), nil })
			x := executorFunc(func(context.Context, Call) (ToolResult, error) {
				count++
				cancel()
				return ToolResult{Output: json.RawMessage(`{"draft":"SO-1"}`)}, nil
			})
			var entries []AuditEntry
			a := auditFunc(func(ctx context.Context, entry AuditEntry) error {
				assert.NoError(t, ctx.Err())
				entries = append(entries, entry)
				if beforeTool && entry.Phase == "started" {
					cancel()
				}
				return nil
			})
			r, err := NewRun(spec(), p, x, a, DefaultLimits())
			require.NoError(t, err)
			result, err := r.Execute(ctx)
			require.NoError(t, err)
			assert.Equal(t, Cancelled, result.Status)
			require.Len(t, entries, 2)
			if beforeTool {
				assert.Zero(t, count)
				assert.Equal(t, "cancelled", entries[1].Phase)
			} else {
				assert.Equal(t, 1, count)
				assert.Equal(t, "completed", entries[1].Phase)
			}
		})
	}
}
