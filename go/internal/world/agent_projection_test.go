package world

import (
	"encoding/json"
	"testing"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentWorkUsesLatestPhaseAndCannotCrossVisibilityBoundary(t *testing.T) {
	e := fixture(t, "agent.run.started")
	e.Data = json.RawMessage(`{"agent_key":"helper","state":"running","code":"OK","department":"warehouse","activity":"working"}`)
	s, err := NewState(*e.Company)
	require.NoError(t, err)
	p, err := NewProjector()
	require.NoError(t, err)
	s, err = p.Apply(s, e)
	require.NoError(t, err)
	require.Empty(t, VisibleEntities(s, auth.Claims{Subject: "sales@test", Companies: []string{*e.Company}, Roles: []string{"Sales User"}}))
	viewer := auth.Claims{Subject: "admin@test", Companies: []string{*e.Company}, Roles: []string{"System Manager"}}
	visible := VisibleEntities(s, viewer)
	require.Len(t, visible, 1)
	require.Contains(t, string(visible[0].Data), "warehouse")
	e.ID = "01K6YEWD6TCY28R0D1FVXY3SYA"
	e.Seq++
	e.Data = json.RawMessage(`{"agent_key":"helper","state":"running","code":"OK","department":"warehouse","activity":"idle"}`)
	s, err = p.Apply(s, e)
	require.NoError(t, err)
	visible = VisibleEntities(s, viewer)
	require.Len(t, visible, 1)
	require.Contains(t, string(visible[0].Data), "idle")
	require.NotContains(t, string(visible[0].Data), "working")
}

func TestAgentProjectionAndSnapshotRemainRoleAndCompanyScoped(t *testing.T) {
	for _, typ := range []string{"agent.run.started", "agent.run.completed", "agent.run.needs_human", "agent.run.failed", "agent.progress.updated", "dev.task.started", "dev.task.updated", "dev.task.completed", "dev.task.failed"} {
		t.Run(typ, func(t *testing.T) {
			e := fixture(t, typ)
			s, err := NewState(*e.Company)
			require.NoError(t, err)
			p, err := NewProjector()
			require.NoError(t, err)
			s, err = p.Apply(s, e)
			require.NoError(t, err)
			require.Len(t, s.Entities(), 1)
			assert.Empty(t, VisibleEntities(s, auth.Claims{Subject: "human@test", Companies: []string{*e.Company}, Roles: []string{"Sales User"}}))
			assert.Empty(t, VisibleEntities(s, auth.Claims{Subject: "admin@test", Companies: []string{"OTHER"}, Roles: []string{"System Manager"}}))
			assert.Len(t, VisibleEntities(s, auth.Claims{Subject: "admin@test", Companies: []string{*e.Company}, Roles: []string{"System Manager"}}), 1)
			raw, err := EncodeSnapshot(s)
			require.NoError(t, err)
			restored, err := DecodeSnapshot(raw)
			require.NoError(t, err)
			assert.Equal(t, s.Entities(), restored.Entities())
		})
	}
}
