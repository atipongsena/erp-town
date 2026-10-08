package erp

import (
	"encoding/json"
	"fmt"
	"time"
)

// Date is a calendar date without time zone, written YYYY-MM-DD.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// ParseDate accepts exactly "2006-01-02" for a real calendar date.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, fmt.Errorf("erp: invalid date %q: %w", s, err)
	}
	d := Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
	if d.String() != s {
		return Date{}, fmt.Errorf("erp: invalid date %q", s)
	}
	return d, nil
}

// MustDate is ParseDate that panics; for constants and tests.
func MustDate(s string) Date {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day) }

func (d Date) IsZero() bool { return d == Date{} }

func (d Date) time() time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

// AddDays returns the date n days later (n may be negative).
func (d Date) AddDays(n int) Date {
	t := d.time().AddDate(0, 0, n)
	return Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}

// Before reports whether d is strictly earlier than o.
func (d Date) Before(o Date) bool { return d.time().Before(o.time()) }

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	p, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = p
	return nil
}
