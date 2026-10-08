package events

import (
	"time"
	_ "time/tzdata" // Asia/Bangkok must resolve on hosts without a zoneinfo database
)

// bangkok is the town's system time zone; envelope times carry its offset whatever
// the host clock's zone is.
var bangkok = mustLoadLocation("Asia/Bangkok")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err) // embedded tzdata always has it
	}
	return loc
}

// FormatBangkok formats t as an envelope time: RFC 3339 at second precision in
// Asia/Bangkok, e.g. 2026-10-09T10:04:05+07:00 (the Frappe outbox's format).
func FormatBangkok(t time.Time) string {
	return t.In(bangkok).Format(time.RFC3339)
}
