// Package agents contains the provider-independent agent run loop. Production
// provider, policy, durable audit and job adapters are deliberately separate.
package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"time"
)

type Limits struct {
	MaxTurns, MaxToolCalls int
	MaxTokens              int64
	MaxPayloadBytes        int
	Timeout                time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxTurns: 8, MaxToolCalls: 16, MaxTokens: 12000, MaxPayloadBytes: 64 * 1024, Timeout: 2 * time.Minute}
}

// ToolDefinition advertises a capability from trusted server configuration.
// Providers never receive ERP credentials or a Port through this contract.
type ToolDefinition struct {
	Name, Description string
	InputSchema       json.RawMessage
}

type Spec struct {
	ID, Instruction string
	Tools           []ToolDefinition
}

type Call struct {
	ID, Tool  string
	Arguments json.RawMessage
}

type ToolResult struct {
	Output     json.RawMessage
	NeedsHuman bool
}

type CallResult struct {
	ID     string
	Output json.RawMessage
}

type Turn struct {
	Text    string
	Calls   []Call
	Results []CallResult
}

type Request struct {
	RunID, Instruction string
	Tools              []ToolDefinition
	History            []Turn
	RemainingTokens    int64
}

type Usage struct{ InputTokens, OutputTokens int64 }

type Response struct {
	Text  string
	Calls []Call
	Usage Usage
}

// Provider performs one model step and must honor ctx and RemainingTokens.
// Responses contain native tool intents, never executable code or ERP access.
type Provider interface {
	Step(context.Context, Request) (Response, error)
}

// Executor is the only tool dispatch path. A production implementation must be
// the policy proxy: validate inputs, bind identity/company server-side, apply
// limits, audit durably and call erp.Port. Never pass raw provider SDK tools here.
type Executor interface {
	Execute(context.Context, Call) (ToolResult, error)
}

type AuditEntry struct {
	RunID, CallID, Tool, Phase string
}

// Audit records intent before a tool and its outcome afterwards. Entries exclude
// prompts, arguments, output and raw errors. Production requires a durable audit;
// the runtime's replay protection alone does not survive process restart.
type Audit interface {
	Record(context.Context, AuditEntry) error
}

type Status string

const (
	Completed  Status = "completed"
	NeedsHuman Status = "needs_human"
	Cancelled  Status = "cancelled"
	Failed     Status = "failed"
)

type Reason string

const (
	TurnLimit           Reason = "TURN_LIMIT"
	ToolLimit           Reason = "TOOL_LIMIT"
	TokenLimit          Reason = "TOKEN_LIMIT"
	PayloadLimit        Reason = "PAYLOAD_LIMIT"
	InvalidResponse     Reason = "INVALID_RESPONSE"
	UnknownTool         Reason = "UNKNOWN_TOOL"
	CallConflict        Reason = "CALL_CONFLICT"
	ToolUncertain       Reason = "TOOL_UNCERTAIN"
	HumanRequired       Reason = "HUMAN_REQUIRED"
	AuditUnavailable    Reason = "AUDIT_UNAVAILABLE"
	ProviderUnavailable Reason = "PROVIDER_UNAVAILABLE"
)

type Result struct {
	Status           Status
	Reason           Reason
	Text             string
	Turns, ToolCalls int
	TokensUsed       int64
}

var (
	ErrInvalidRun      = errors.New("agents: invalid run configuration")
	ErrAlreadyExecuted = errors.New("agents: run already executed")
)

// Run is single-use, even under concurrent Execute calls. Creating another Run
// with the same id is not cross-process idempotency; durable run claiming is still
// required before production execution or River retries.
type Run struct {
	spec     Spec
	provider Provider
	executor Executor
	audit    Audit
	limits   Limits
	started  atomic.Bool
}

func NewRun(spec Spec, provider Provider, executor Executor, audit Audit, limits Limits) (*Run, error) {
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	if provider == nil || executor == nil || audit == nil || !validID(spec.ID) || strings.TrimSpace(spec.Instruction) == "" ||
		limits.MaxTurns <= 0 || limits.MaxToolCalls <= 0 || limits.MaxTokens <= 0 || limits.MaxPayloadBytes <= 0 || limits.Timeout <= 0 {
		return nil, ErrInvalidRun
	}
	names := make(map[string]bool, len(spec.Tools))
	for _, tool := range spec.Tools {
		if !validID(tool.Name) || names[tool.Name] || !jsonObject(tool.InputSchema) {
			return nil, ErrInvalidRun
		}
		names[tool.Name] = true
	}
	spec.Tools = copyTools(spec.Tools)
	r := &Run{spec: spec, provider: provider, executor: executor, audit: audit, limits: limits}
	if !r.fits(Request{RunID: spec.ID, Instruction: spec.Instruction, Tools: spec.Tools, RemainingTokens: limits.MaxTokens}) {
		return nil, ErrInvalidRun
	}
	return r, nil
}

func (r *Run) Execute(ctx context.Context) (Result, error) {
	if !r.started.CompareAndSwap(false, true) {
		return Result{}, ErrAlreadyExecuted
	}
	ctx, cancel := context.WithTimeout(ctx, r.limits.Timeout)
	defer cancel()
	result := Result{}
	stop := func(status Status, reason Reason) (Result, error) {
		result.Status, result.Reason = status, reason
		return result, nil
	}
	allowed := make(map[string]bool, len(r.spec.Tools))
	for _, tool := range r.spec.Tools {
		allowed[tool.Name] = true
	}
	history := []Turn{}
	completed := make(map[string]cachedCall)
	for result.Turns < r.limits.MaxTurns {
		if ctx.Err() != nil {
			return stop(Cancelled, "")
		}
		if result.TokensUsed >= r.limits.MaxTokens {
			return stop(NeedsHuman, TokenLimit)
		}
		req := Request{
			RunID: r.spec.ID, Instruction: r.spec.Instruction, Tools: copyTools(r.spec.Tools),
			History: copyHistory(history), RemainingTokens: r.limits.MaxTokens - result.TokensUsed,
		}
		if !r.fits(req) {
			return stop(NeedsHuman, PayloadLimit)
		}
		response, err := invoke(func() (Response, error) { return r.provider.Step(ctx, req) })
		result.Turns++
		if ctx.Err() != nil {
			return stop(Cancelled, "")
		}
		if err != nil {
			return stop(Failed, ProviderUnavailable)
		}
		usage := response.Usage
		if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.InputTokens > math.MaxInt64-usage.OutputTokens {
			return stop(NeedsHuman, InvalidResponse)
		}
		tokens := usage.InputTokens + usage.OutputTokens
		if tokens > math.MaxInt64-result.TokensUsed {
			return stop(NeedsHuman, InvalidResponse)
		}
		result.TokensUsed += tokens
		if result.TokensUsed > r.limits.MaxTokens {
			return stop(NeedsHuman, TokenLimit)
		}
		if !r.fits(response) {
			// Invalid raw JSON is a protocol error rather than a payload overflow.
			if _, err := json.Marshal(response); err != nil {
				return stop(NeedsHuman, InvalidResponse)
			}
			return stop(NeedsHuman, PayloadLimit)
		}
		if len(response.Calls) == 0 {
			if strings.TrimSpace(response.Text) == "" {
				return stop(NeedsHuman, InvalidResponse)
			}
			result.Text = response.Text
			return stop(Completed, "")
		}
		calls, reason := r.validateBatch(response.Calls, completed, allowed, result.ToolCalls)
		if reason != "" {
			return stop(NeedsHuman, reason)
		}
		turn := Turn{Text: response.Text, Calls: calls}
		for _, call := range calls {
			if ctx.Err() != nil {
				return stop(Cancelled, "")
			}
			if cached, ok := completed[call.ID]; ok {
				turn.Results = append(turn.Results, CallResult{ID: call.ID, Output: bytes.Clone(cached.output)})
				continue
			}
			if err := r.record(ctx, call, "started", false); err != nil {
				return stop(NeedsHuman, AuditUnavailable)
			}
			if ctx.Err() != nil {
				if err := r.record(ctx, call, "cancelled", true); err != nil {
					return stop(NeedsHuman, AuditUnavailable)
				}
				return stop(Cancelled, "")
			}
			result.ToolCalls++
			out, err := invoke(func() (ToolResult, error) { return r.executor.Execute(ctx, copyCall(call)) })
			phase, reason := "completed", Reason("")
			if err != nil || !json.Valid(out.Output) || !r.fits(out) {
				phase, reason = "uncertain", ToolUncertain
			} else if out.NeedsHuman {
				phase, reason = "needs_human", HumanRequired
			}
			// The tool may have committed: record the outcome even on cancellation.
			if err := r.record(ctx, call, phase, true); err != nil {
				return stop(NeedsHuman, AuditUnavailable)
			}
			if reason != "" {
				return stop(NeedsHuman, reason)
			}
			completed[call.ID] = cachedCall{tool: call.Tool, arguments: string(call.Arguments), output: bytes.Clone(out.Output)}
			turn.Results = append(turn.Results, CallResult{ID: call.ID, Output: bytes.Clone(out.Output)})
		}
		history = append(history, turn)
	}
	if ctx.Err() != nil {
		return stop(Cancelled, "")
	}
	return stop(NeedsHuman, TurnLimit)
}

type cachedCall struct {
	tool, arguments string
	output          json.RawMessage
}

func (r *Run) validateBatch(input []Call, completed map[string]cachedCall, allowed map[string]bool, spent int) ([]Call, Reason) {
	calls := make([]Call, 0, len(input))
	seen := make(map[string]Call, len(input))
	newCalls := 0
	for _, raw := range input {
		if !validID(raw.ID) || !validID(raw.Tool) || !jsonObject(raw.Arguments) {
			return nil, InvalidResponse
		}
		if !allowed[raw.Tool] {
			return nil, UnknownTool
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw.Arguments); err != nil {
			return nil, InvalidResponse
		}
		call := Call{ID: raw.ID, Tool: raw.Tool, Arguments: bytes.Clone(compact.Bytes())}
		if earlier, ok := seen[call.ID]; ok {
			if earlier.Tool != call.Tool || !bytes.Equal(earlier.Arguments, call.Arguments) {
				return nil, CallConflict
			}
			continue
		}
		if cached, ok := completed[call.ID]; ok {
			if cached.tool != call.Tool || cached.arguments != string(call.Arguments) {
				return nil, CallConflict
			}
		} else {
			newCalls++
		}
		seen[call.ID] = call
		calls = append(calls, call)
	}
	if newCalls > r.limits.MaxToolCalls-spent {
		return nil, ToolLimit
	}
	return calls, ""
}

func (r *Run) record(ctx context.Context, call Call, phase string, detached bool) error {
	if detached {
		ctx = context.WithoutCancel(ctx)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := invoke(func() (struct{}, error) {
		return struct{}{}, r.audit.Record(ctx, AuditEntry{RunID: r.spec.ID, CallID: call.ID, Tool: call.Tool, Phase: phase})
	})
	return err
}

// Contain only adapter panics, including panic(nil), without recording their
// potentially sensitive payload. Runtime invariant failures are not swallowed.
func invoke[T any](f func() (T, error)) (out T, err error) {
	returned := false
	defer func() {
		if !returned {
			_ = recover()
			err = errors.New("agents: adapter panicked")
		}
	}()
	out, err = f()
	returned = true
	return out, err
}

func (r *Run) fits(v any) bool {
	b, err := json.Marshal(v)
	return err == nil && len(b) <= r.limits.MaxPayloadBytes
}

func jsonObject(b []byte) bool {
	b = bytes.TrimSpace(b)
	return len(b) > 0 && b[0] == '{' && json.Valid(b)
}

func validID(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return true
}

func copyCall(c Call) Call { c.Arguments = bytes.Clone(c.Arguments); return c }

func copyTools(tools []ToolDefinition) []ToolDefinition {
	out := append([]ToolDefinition(nil), tools...)
	for i := range out {
		out[i].InputSchema = bytes.Clone(out[i].InputSchema)
	}
	return out
}

func copyHistory(history []Turn) []Turn {
	out := append([]Turn(nil), history...)
	for i := range out {
		out[i].Calls = append([]Call(nil), out[i].Calls...)
		for j := range out[i].Calls {
			out[i].Calls[j] = copyCall(out[i].Calls[j])
		}
		out[i].Results = append([]CallResult(nil), out[i].Results...)
		for j := range out[i].Results {
			out[i].Results[j].Output = bytes.Clone(out[i].Results[j].Output)
		}
	}
	return out
}
