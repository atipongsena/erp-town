package world

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/events"
	"github.com/stretchr/testify/require"
)

type historyFixture struct {
	events    []events.Envelope
	calls     int
	scanLimit int
}

func (s *historyFixture) Read(_ context.Context, _, _ time.Time, limit int) ([]events.Envelope, bool, error) {
	s.calls++
	s.scanLimit = limit
	return s.events, false, nil
}

func TestHistoryRejectsExpiredAccessAndBoundsRetainedResults(t *testing.T) {
	e := fixture(t, "erp.sales_order.submitted")
	now := time.Now()
	e.Time = now.Format(time.RFC3339Nano)
	source := &historyFixture{}
	for range 1001 {
		source.events = append(source.events, e)
	}
	h := NewHistory(source)
	h.now = func() time.Time { return now }
	claims := auth.Claims{Subject: "human@test", Companies: []string{*e.Company}, Roles: e.Visibility.Roles, ExpiresAt: now.Add(time.Minute)}
	out, err := h.Read(t.Context(), claims, *e.Company, now.Add(-time.Minute), now, 10)
	require.NoError(t, err)
	require.Len(t, out.Transitions, 10)
	require.True(t, out.Truncated)
	require.Equal(t, 1001, source.scanLimit)
	claims.ExpiresAt = now
	before := source.calls
	_, err = h.Read(t.Context(), claims, *e.Company, now.Add(-time.Minute), now, 10)
	require.ErrorIs(t, err, ErrHistoryDenied)
	require.Equal(t, before, source.calls)
}

func TestHistoryPrivateOwnershipAndNoDeveloperOrProgressTracking(t *testing.T) {
	now := time.Now()
	e := fixture(t, "erp.sales_order.submitted")
	e.Time = now.Format(time.RFC3339Nano)
	e.Actor = events.Actor{Kind: "user", ID: "user.owner@test"}
	e.Visibility = events.Visibility{Level: "private", Roles: []string{}}
	source := &historyFixture{events: []events.Envelope{e}}
	for _, typ := range []string{"dev.task.started", "agent.progress.updated"} {
		excluded := fixture(t, typ)
		excluded.Company = e.Company
		excluded.Time = e.Time
		source.events = append(source.events, excluded)
	}
	h := NewHistory(source)
	h.now = func() time.Time { return now }
	claims := auth.Claims{Subject: "owner@test", Companies: []string{*e.Company}, Roles: []string{"System Manager"}, ExpiresAt: now.Add(time.Minute)}
	out, err := h.Read(t.Context(), claims, *e.Company, now.Add(-time.Minute), now, 100)
	require.NoError(t, err)
	require.Len(t, out.Transitions, 1)
	claims.Subject = "other@test"
	out, err = h.Read(t.Context(), claims, *e.Company, now.Add(-time.Minute), now, 100)
	require.NoError(t, err)
	require.Empty(t, out.Transitions)
}

func TestHistoryCurrentCompanyVisibilityBoundsAndPrivacy(t *testing.T) {
	var e events.Envelope
	require.NoError(t, json.Unmarshal([]byte(historyOrder), &e))
	source := &historyFixture{events: []events.Envelope{e}}
	h := NewHistory(source)
	now := time.Now()
	claims := auth.Claims{Subject: "human@test", Companies: []string{"ACME"}, Roles: []string{"Sales User"}, ExpiresAt: now.Add(time.Minute)}
	e.Company = new("ACME")
	e.ID = "01JA0K6Y8Z9P3R4S5T6V7W8XA1"
	e.Time = now.Format(time.RFC3339Nano)
	e.Source = "erp/selling"
	e.Data = json.RawMessage(`{"status":"To Deliver and Bill","party":"CUST","item_count":1,"delivery_date":"2026-10-11"}`)
	e.Visibility = events.Visibility{Level: "roles", Roles: []string{"Sales User"}}
	source.events = []events.Envelope{e}
	validator, err := events.NewValidator()
	require.NoError(t, err)
	encoded, err := events.Marshal(e)
	require.NoError(t, err)
	require.NoError(t, validator.Validate(encoded))
	out, err := h.Read(t.Context(), claims, "ACME", now.Add(-time.Minute), now, 100)
	require.NoError(t, err)
	require.Len(t, out.Transitions, 1)
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	for _, secret := range []string{"grand_total", "customer", "actor", "human@test", "on_behalf_of", "amount", "qty"} {
		require.NotContains(t, string(raw), secret)
	}
	claims.Roles = []string{"Stock User"}
	out, err = h.Read(t.Context(), claims, "ACME", now.Add(-time.Minute), now, 100)
	require.NoError(t, err)
	require.Empty(t, out.Transitions)
	before := source.calls
	_, err = h.Read(t.Context(), claims, "OTHER", now.Add(-time.Minute), now, 100)
	require.Error(t, err)
	require.Equal(t, before, source.calls)
	_, err = h.Read(t.Context(), claims, "ACME", now.Add(-25*time.Hour), now, 100)
	require.Error(t, err)
	_, err = h.Read(t.Context(), claims, "ACME", now.Add(-time.Minute), now, 1001)
	require.Error(t, err)
}

const historyOrder = `{"id":"01J000000000000000000000000","type":"erp.sales_order.submitted","source":"erpnext","time":"2026-10-11T00:00:00Z","company":"ACME","actor":{"kind":"user","id":"user.human@test","on_behalf_of":null},"subject":{"doctype":"Sales Order","name":"SO-1"},"seq":1,"correlation_id":"01J000000000000000000000000","causation_id":null,"visibility":{"level":"roles","roles":["Sales User"]},"data":{"customer":"CUST","grand_total":"107.00","currency":"THB","items":[{"item_code":"ITEM","qty":"1","rate":"100.00","amount":"100.00"}]},"specversion":"1.0","schema_version":1}`
