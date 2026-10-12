//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/atipongsena/erp-town/go/internal/agents"
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/erpnext"
	"github.com/atipongsena/erp-town/go/internal/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type localAgentProvider struct{ call agents.Call }

func (p localAgentProvider) Step(_ context.Context, req agents.Request) (agents.Response, error) {
	if len(req.History) == 0 {
		return agents.Response{Calls: []agents.Call{p.call}, Usage: agents.Usage{InputTokens: 1, OutputTokens: 1}}, nil
	}
	return agents.Response{Text: "The draft is ready for review.", Usage: agents.Usage{InputTokens: 1, OutputTokens: 1}}, nil
}

func TestAgentLive(t *testing.T) {
	e := testEnv(t)
	require.Equal(t, "test.localhost", e.site)
	var keys struct {
		AgentKey         string `json:"agent_key"`
		Agent, Delegator struct{ User, Key, Secret string }
	}
	raw := os.Getenv("TOWN_TEST_AGENT_KEYS")
	if raw == "" {
		t.Skip("run node tests/integration/agent-acceptance.ts")
	}
	// Do not print raw credentials, including on an unmarshal/assertion failure.
	if json.Unmarshal([]byte(raw), &keys) != nil || keys.Agent.Key == "" || keys.Agent.Secret == "" || keys.Delegator.Key == "" || keys.Delegator.Secret == "" {
		t.Fatal("invalid isolated agent credentials")
	}
	adapter := erpnext.NewAdapter(erpnext.NewClient(erpnext.ClientConfig{BaseURL: e.erpURL, Site: e.site}))
	pool := storetest.NewPool(t)
	ledger := agents.NewPostgresLedger(pool)
	binding := agents.Binding{RunID: "LIVE-AGENT-1", AgentKey: keys.AgentKey, Company: "ACME-TH", AgentCredential: erp.APIKeyCredential{Key: keys.Agent.Key, Secret: keys.Agent.Secret}, DelegatorCredential: erp.APIKeyCredential{Key: keys.Delegator.Key, Secret: keys.Delegator.Secret}}
	executor, err := agents.NewPolicyExecutor(adapter, ledger, binding)
	require.NoError(t, err)
	require.NoError(t, executor.Validate(t.Context()))
	require.NoError(t, ledger.Create(t.Context(), binding.RunID, binding.AgentKey, binding.Company, keys.Delegator.User))
	state, err := ledger.Start(t.Context(), binding.RunID)
	require.NoError(t, err)
	require.Equal(t, agents.RunClaimNew, state)
	args, _ := json.Marshal(map[string]any{"customer": "CUST-001", "transaction_date": bangkokToday(), "delivery_date": bangkokToday(), "lines": []map[string]string{{"item_code": "ITEM-001", "qty": "1"}}})
	call := agents.Call{ID: "draft-1", Tool: "create_sales_order_draft", Arguments: args}
	run, err := agents.NewRun(agents.Spec{ID: binding.RunID, Instruction: "Prepare the requested order draft.", Tools: executor.Tools()}, localAgentProvider{call: call}, executor, ledger, agents.DefaultLimits())
	require.NoError(t, err)
	result, err := run.Execute(t.Context())
	require.NoError(t, err)
	require.Equal(t, agents.Completed, result.Status)
	require.NoError(t, ledger.CompleteRun(t.Context(), binding.RunID, result))
	// A new executor after process restart returns the recorded reference and
	// never repeats the ERP insert, even though the run has already terminated.
	restarted, err := agents.NewPolicyExecutor(adapter, agents.NewPostgresLedger(pool), binding)
	require.NoError(t, err)
	output, err := restarted.Execute(t.Context(), call)
	require.NoError(t, err)
	require.False(t, output.NeedsHuman)
	var safe agents.SafeResult
	require.NoError(t, json.Unmarshal(output.Output, &safe))
	require.Equal(t, erp.DocDraft, safe.Status)
	order, err := adapter.GetSalesOrder(t.Context(), erp.Principal{Credential: binding.DelegatorCredential}, safe.Ref.Name)
	require.NoError(t, err)
	assert.Equal(t, binding.Company, order.Company)
	assert.Equal(t, erp.DocDraft, order.Status)
	var calls int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM agent_tool_calls").Scan(&calls))
	assert.Equal(t, 1, calls)
	forged := call
	forged.ID = "forged"
	forged.Arguments = json.RawMessage(`{"company":"ETT","customer":"CUST-001"}`)
	output, err = restarted.Execute(t.Context(), forged)
	require.NoError(t, err)
	assert.True(t, output.NeedsHuman)
	forbidden := binding
	forbidden.Company = "ETT"
	other, err := agents.NewPolicyExecutor(adapter, ledger, forbidden)
	require.NoError(t, err)
	require.Error(t, other.Validate(t.Context()))
	output, err = restarted.Execute(t.Context(), agents.Call{ID: "submit", Tool: "submit", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	assert.True(t, output.NeedsHuman)
}
