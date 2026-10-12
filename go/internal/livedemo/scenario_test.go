package livedemo

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/erpfake"
)

func TestDemoAdvertisesContinuousPresentationWithoutRepeatingERP(t *testing.T) {
	s := testScenario(t, 1)
	if err := s.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state["motion_mode"] != "loop" || len(s.Snapshot().Documents) != 4 || s.Snapshot().Stage != "completed" {
		t.Fatal("demo must advertise visual looping while its ERP workflow remains bounded")
	}
}

func testScenario(t *testing.T, rounds int) *Scenario {
	t.Helper()
	users := map[string]erpfake.FakeUser{}
	staff := map[string]erp.Principal{}
	for d, role := range map[string]string{"shop": "Sales User", "warehouse": "Stock User", "bank": "Accounts User"} {
		users[d] = erpfake.FakeUser{User: d, Roles: []string{role}, Companies: []string{"ACME-TH"}}
		staff[d] = erp.Principal{ID: d, Credential: erp.SessionCredential{SessionID: d}}
	}
	port := erpfake.New(erpfake.Config{Company: "ACME-TH", Prices: map[string]erp.Decimal{"ITEM-001": erp.MustDecimal("100")}, Users: users})
	draft := func(ctx context.Context, _ int, date erp.Date) (erp.DocRef, error) {
		return port.CreateSalesOrder(ctx, staff["shop"], erp.SalesOrderInput{Company: "ACME-TH", Customer: "CUST-001", TransactionDate: date, DeliveryDate: date, Lines: []erp.LineInput{{ItemCode: "ITEM-001", Qty: erp.MustDecimal("1")}}})
	}
	s, err := New(port, staff, draft, rounds)
	if err != nil {
		t.Fatal(err)
	}
	s.wait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	s.review = 0
	return s
}

func TestBoundedSalesToCash(t *testing.T) {
	s := testScenario(t, 3)
	if err := s.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	state := s.Snapshot()
	if state.Round != 3 || state.Stage != "completed" || len(state.Documents) != 12 {
		t.Fatalf("wrong completed state: %#v", state)
	}
	for _, d := range state.Documents {
		if d.Status != "Submitted" {
			t.Fatalf("unsubmitted real port document: %#v", d)
		}
	}
	for _, a := range state.Actors {
		if a.Activity != "idle" {
			t.Fatalf("actor still working after actual completion: %#v", a)
		}
	}
}

func TestScheduledDeadlinePrecedesERPAndSnapshotsOwnTravel(t *testing.T) {
	s := testScenario(t, 1)
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	first := true
	s.wait = func(ctx context.Context, duration time.Duration) error {
		state := s.Snapshot()
		if !state.StageStartedAt.Equal(now) {
			t.Fatal("stage does not use the injected clock")
		}
		deadline := now.Add(duration)
		for i, actor := range state.Actors {
			if actor.Travel == nil {
				continue
			}
			end := actor.Travel.StartedAt.Add(time.Duration(actor.Travel.DurationMS) * time.Millisecond)
			if end.After(deadline) {
				t.Fatal("actor arrival is later than the operation deadline")
			}
			actor.Travel.DurationMS = -1
			if s.Snapshot().Actors[i].Travel.DurationMS < 0 {
				t.Fatal("snapshot aliases travel")
			}
		}
		if first {
			first = false
			route, err := RouteFor("it", "shop", "agent")
			if err != nil || duration != TravelDuration(route) || len(state.Documents) != 0 {
				t.Fatal("first ERP operation did not wait for its route")
			}
		}
		now = deadline
		return ctx.Err()
	}
	if err := s.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, actor := range s.Snapshot().Actors {
		if actor.Travel != nil || actor.TaskRef != nil || actor.Activity != "idle" {
			t.Fatal("completion retains work resources")
		}
	}
}

func TestStageReleasesInactiveTaskReferences(t *testing.T) {
	s := testScenario(t, 1)
	ref := erp.DocRef{DocType: erp.DocSalesOrder, Name: "SO-private"}
	s.stage("review", "working", "demo-shop", "shop", 0, &ref)
	s.stage("next", "working", "demo-bank", "bank", 0, nil)
	if s.Snapshot().Actors[0].TaskRef != nil {
		t.Fatal("idle actor retains an earlier task")
	}
}

func TestUncertainWriteHaltsWithoutRetry(t *testing.T) {
	s := testScenario(t, 3)
	calls := 0
	s.agentDraft = func(context.Context, int, erp.Date) (erp.DocRef, error) {
		calls++
		return erp.DocRef{}, errors.New("ambiguous insert response")
	}
	if s.Run(t.Context()) == nil {
		t.Fatal("expected stop")
	}
	state := s.Snapshot()
	if calls != 1 || state.Round != 1 || state.Phase != "failed" || state.Error != "ERP_STEP_FAILED" || len(state.Documents) != 0 {
		t.Fatalf("unsafe retry or false success: %#v calls=%d", state, calls)
	}
	for _, actor := range state.Actors {
		if actor.Activity != "idle" {
			t.Fatal("failure leaves a fake working actor")
		}
	}
}

func TestTravellingTaskPrecedesERPWriteAndSnapshotIsIndependent(t *testing.T) {
	s := testScenario(t, 1)
	s.wait = pause
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for s.Snapshot().Phase != "travelling" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	state := s.Snapshot()
	if state.Phase != "travelling" || len(state.Documents) != 0 || state.Actors[3].Department != "shop" || state.Actors[3].FromDepartment != "it" {
		t.Fatalf("travel was not pre-operation: %#v", state)
	}
	state.Actors[3].Department = "bank"
	if s.Snapshot().Actors[3].Department != "shop" {
		t.Fatal("snapshot mutates server state")
	}
	cancel()
	if !errors.Is(<-done, context.Canceled) {
		t.Fatal("travel does not honor cancellation")
	}
}
