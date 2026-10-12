package agents

import (
	"encoding/json"
	"testing"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/stretchr/testify/require"
)

func TestToolClaimAndFinishPublishOnlyLiveCoarseWork(t *testing.T) {
	l, in := ledgerFixture(t)
	in.Tool = "create_delivery_note_draft"
	claim, err := l.Claim(t.Context(), in)
	require.NoError(t, err)
	require.Equal(t, ClaimNew, claim.State)
	latest := func() map[string]any {
		t.Helper()
		var raw string
		require.NoError(t, l.pool.QueryRow(t.Context(), "SELECT payload FROM town_outbox WHERE subject_doctype='Town Agent Run' ORDER BY insert_order DESC LIMIT 1").Scan(&raw))
		v, err := events.NewValidator()
		require.NoError(t, err)
		e, err := events.Decode(v, []byte(raw))
		require.NoError(t, err)
		require.Equal(t, []string{"System Manager"}, e.Visibility.Roles)
		require.Equal(t, "ACME", *e.Company)
		require.Nil(t, e.Actor.OnBehalfOf)
		var data map[string]any
		require.NoError(t, json.Unmarshal(e.Data, &data))
		require.Len(t, data, 5)
		return data
	}
	data := latest()
	require.Equal(t, "warehouse", data["department"])
	require.Equal(t, "working", data["activity"])
	require.Equal(t, "running", data["state"])
	var before int
	require.NoError(t, l.pool.QueryRow(t.Context(), "SELECT count(*) FROM town_outbox").Scan(&before))
	_, err = l.Claim(t.Context(), in)
	require.NoError(t, err)
	var after int
	require.NoError(t, l.pool.QueryRow(t.Context(), "SELECT count(*) FROM town_outbox").Scan(&after))
	require.Equal(t, before, after, "retry must not restart an active tool")
	require.NoError(t, l.Finish(t.Context(), in, SafeResult{Ref: erp.DocRef{DocType: erp.DocDeliveryNote, Name: "PRIVATE-DN"}, Status: erp.DocDraft, Code: "OK"}))
	data = latest()
	require.Equal(t, "idle", data["activity"])
	require.Equal(t, "warehouse", data["department"])
	for _, key := range []string{"ref", "name", "prompt", "tool", "amount", "delegator"} {
		require.NotContains(t, data, key)
	}
}

func TestFinishingToolAfterRunStoppedCannotRestartWork(t *testing.T) {
	l, in := ledgerFixture(t)
	_, err := l.Claim(t.Context(), in)
	require.NoError(t, err)
	require.NoError(t, l.CompleteRun(t.Context(), in.RunID, Result{Status: Cancelled}))
	require.NoError(t, l.Finish(t.Context(), in, SafeResult{Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: "SO-1"}, Status: erp.DocDraft, Code: "OK"}))
	var raw string
	require.NoError(t, l.pool.QueryRow(t.Context(), "SELECT payload FROM town_outbox WHERE subject_doctype='Town Agent Run' ORDER BY insert_order DESC LIMIT 1").Scan(&raw))
	require.Contains(t, raw, "needs_human")
	require.NotContains(t, raw, `"activity":"working"`)
}

func TestWorkDepartmentUsesOnlyAdvertisedCapabilities(t *testing.T) {
	for tool, department := range map[string]string{
		"read_sales_order": "shop", "create_sales_order_draft": "shop",
		"read_delivery_note": "warehouse", "create_delivery_note_draft": "warehouse",
		"read_sales_invoice": "bank", "create_sales_invoice_draft": "bank",
		"read_payment_entry": "bank", "create_payment_draft": "bank",
		"submit_payment": "", "read_payroll": "", "provider supplied warehouse": "",
	} {
		require.Equal(t, department, agentWorkDepartment(tool), tool)
	}
}

func TestWorkPublicationFailureRollsBackToolClaim(t *testing.T) {
	l, in := ledgerFixture(t)
	_, err := l.pool.Exec(t.Context(), "DROP TABLE town_outbox")
	require.NoError(t, err)
	_, err = l.Claim(t.Context(), in)
	require.Error(t, err)
	var count int
	require.NoError(t, l.pool.QueryRow(t.Context(), "SELECT count(*) FROM agent_tool_calls").Scan(&count))
	require.Zero(t, count)
	require.NoError(t, l.pool.QueryRow(t.Context(), "SELECT call_count FROM agent_runs WHERE id=$1", in.RunID).Scan(&count))
	require.Zero(t, count)
}
