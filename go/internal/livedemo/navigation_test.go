package livedemo

import (
	"math"
	"testing"
	"time"
)

func TestSharedRouteParity(t *testing.T) {
	for _, row := range layout.Parity {
		route, err := RouteFor(row.From, row.To, row.Kind)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(route.Length-row.Length) > 1e-6 || TravelDuration(route).Milliseconds() != row.DurationMS {
			t.Fatalf("route mismatch %v: %v", row, route)
		}
	}
	if _, err := RouteFor("missing", "shop", "human"); err == nil {
		t.Fatal("invalid department accepted")
	}
}

func TestScheduledReturnsAreBoundedAndNonOverlapping(t *testing.T) {
	start := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	trips, err := ScheduleTravel([]TravelRequest{{ID: "b", From: "bank", To: "hq", Kind: "human"}, {ID: "a", From: "shop", To: "hq", Kind: "human"}, {ID: "idle", From: "it", To: "it", Kind: "agent"}}, start)
	if err != nil {
		t.Fatal(err)
	}
	if trips[0].ID != "a" || trips[1].StartedAt.Before(trips[0].StartedAt.Add(trips[0].Duration)) {
		t.Fatal("conflicting return windows")
	}
	if trips[2].Duration != 0 {
		t.Fatal("zero-distance travel should not wait")
	}
}
