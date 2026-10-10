package world

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/events"
)

func TestSnapshotRoundTripPreservesFactsAndTombstones(t *testing.T) {
	p, state := setup(t)
	requested := fixture(t, "workflow.approval.requested")
	state = apply(t, p, state, requested)
	decided := fixture(t, "workflow.approval.decided")
	decided.Subject, decided.Seq = requested.Subject, requested.Seq+1
	state = apply(t, p, state, decided)
	state = apply(t, p, state, fixture(t, "erp.sales_invoice.overdue"))
	raw, err := EncodeSnapshot(state)
	require.NoError(t, err)
	restored, err := DecodeSnapshot(raw)
	require.NoError(t, err)
	assert.Equal(t, state.Company(), restored.Company())
	assert.Equal(t, state.Version(), restored.Version())
	assert.Equal(t, state.Entities(), restored.Entities())
	assert.Equal(t, decided.Seq, restored.LastSeq(requested.Subject))
	replayed := apply(t, p, restored, requested)
	assert.Equal(t, restored.Entities(), replayed.Entities(), "old approval must not resurrect after restart")
	assert.Equal(t, restored.Version(), replayed.Version())
	encodedAgain, err := EncodeSnapshot(restored)
	require.NoError(t, err)
	assert.Equal(t, raw, encodedAgain, "snapshot bytes must be deterministic")
	visible := restored.Entities()
	visible[0].Data[0] = '!'
	assert.Equal(t, state.Entities(), restored.Entities())
}

func TestSnapshotRejectsCorruptState(t *testing.T) {
	p, state := setup(t)
	state = apply(t, p, state, fixture(t, "erp.sales_order.submitted"))
	raw, err := EncodeSnapshot(state)
	require.NoError(t, err)
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"unknown format", func(v map[string]any) { v["format_version"] = 99 }},
		{"blank company", func(v map[string]any) { v["company"] = " " }},
		{"unknown field", func(v map[string]any) { v["unexpected"] = true }},
		{"missing version", func(v map[string]any) { delete(v, "version") }},
		{"missing entities", func(v map[string]any) { delete(v, "entities") }},
		{"missing facet fences", func(v map[string]any) { delete(v, "facet_fences") }},
		{"null facet fences", func(v map[string]any) { v["facet_fences"] = nil }},
		{"null sequences", func(v map[string]any) { v["seqs"] = nil }},
		{"entity version beyond room", func(v map[string]any) { v["version"] = 0 }},
		{"duplicate entity", func(v map[string]any) { es := v["entities"].([]any); v["entities"] = append(es, es[0]) }},
		{"duplicate seq", func(v map[string]any) { ss := v["seqs"].([]any); v["seqs"] = append(ss, ss[0]) }},
		{"missing seq", func(v map[string]any) { v["seqs"] = []any{} }},
		{"stale seq", func(v map[string]any) { v["seqs"].([]any)[0].(map[string]any)["seq"] = 0 }},
		{"wrong kind", func(v map[string]any) {
			v["entities"].([]any)[0].(map[string]any)["Key"].(map[string]any)["Kind"] = "overdue"
		}},
		{"invalid payload", func(v map[string]any) { v["entities"].([]any)[0].(map[string]any)["Data"] = map[string]any{} }},
		{"unknown entity field", func(v map[string]any) { v["entities"].([]any)[0].(map[string]any)["unexpected"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var value map[string]any
			require.NoError(t, json.Unmarshal(raw, &value))
			tc.mutate(value)
			corrupt, err := json.Marshal(value)
			require.NoError(t, err)
			_, err = DecodeSnapshot(corrupt)
			require.Error(t, err)
		})
	}
	for _, invalid := range [][]byte{nil, []byte("null"), append(bytes.Clone(raw), []byte(" {}")...), bytes.Repeat([]byte(" "), 32*1024*1024+1)} {
		_, err := DecodeSnapshot(invalid)
		require.Error(t, err)
	}
	_, err = EncodeSnapshot(State{})
	require.Error(t, err)
}

func TestSnapshotRejectsCorruptFacetFences(t *testing.T) {
	_, state := setup(t)
	state.version = 1
	key := EntityKey{Subject: events.Subject{DocType: "Sales Invoice", Name: "SI-PAID"}, Kind: KindOverdue}
	state.reconciled[key] = 4
	raw, err := EncodeSnapshot(state)
	require.NoError(t, err)
	for _, mutate := range []func(*snapshotData){
		func(s *snapshotData) { s.FacetFences[0].Seq = -1 },
		func(s *snapshotData) { s.FacetFences[0].Seq = 0 },
		func(s *snapshotData) { s.FacetFences[0].Key.Kind = KindApproval },
		func(s *snapshotData) { s.FacetFences[0].Key.Scope = "warehouse" },
		func(s *snapshotData) { s.FacetFences = append(s.FacetFences, s.FacetFences[0]) },
	} {
		var data snapshotData
		require.NoError(t, json.Unmarshal(raw, &data))
		mutate(&data)
		bad, err := json.Marshal(data)
		require.NoError(t, err)
		_, err = DecodeSnapshot(bad)
		require.Error(t, err)
	}
}

func TestSnapshotReconciledFactsAreWhitelisted(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		kind                    EntityKind
		docType, scope, payload string
	}{
		{"document", KindDocument, "Sales Order", "", `{"status":"To Deliver and Bill"}`},
		{"overdue", KindOverdue, "Sales Invoice", "", `{"due_date":"2026-10-08","days_overdue":2}`},
		{"stock", KindStock, "Item", "Main", `{"warehouse":"Main","fill_ratio":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, state := setup(t)
			state.version = 1
			key := EntityKey{Subject: events.Subject{DocType: tc.docType, Name: "RECONCILED"}, Kind: tc.kind, Scope: tc.scope}
			state.seqs[key.Subject] = 0
			entity := Entity{Key: key, Version: 1, EventType: "world.projection." + tc.name, Time: "2026-10-10T12:00:00Z", Actor: events.Actor{Kind: "system", ID: "system.town"}, Visibility: events.Visibility{Level: "public", Roles: []string{}}, Data: json.RawMessage(tc.payload)}
			state.entities[key] = entity
			raw, err := EncodeSnapshot(state)
			require.NoError(t, err)
			restored, err := DecodeSnapshot(raw)
			require.NoError(t, err)
			assert.Equal(t, state.Entities(), restored.Entities())
			for _, payload := range []string{`null`, `{}`, `{"status":"Open","amount":100}`, `{"due_date":"2026-02-30","days_overdue":1}`, `{"warehouse":"Other","fill_ratio":0.5}`, `{"warehouse":"Main","fill_ratio":1.01}`, `{"warehouse":"Main","fill_ratio":0,"fill_ratio":1}`} {
				bad := state.copyForWrite()
				entity.Data = json.RawMessage(payload)
				bad.entities[key] = entity
				_, err := EncodeSnapshot(bad)
				require.Error(t, err, payload)
			}
		})
	}
}
