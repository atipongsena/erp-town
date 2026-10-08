package erp

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/apd/v3"
)

const maxDecimalDigits = 28

// arithCtx has enough headroom that sums and products of parsed values are exact.
var arithCtx = apd.Context{
	Precision:   64,
	MaxExponent: 100,
	MinExponent: -100,
	Traps:       apd.DefaultTraps,
	Rounding:    apd.RoundHalfUp,
}

// Decimal is an exact base-10 number for amounts and quantities. The zero
// value is 0. Compare with Cmp, not ==.
type Decimal struct{ d apd.Decimal }

// ParseDecimal accepts an optional leading '-', digits, and an optional '.'
// followed by digits (no exponent, no '+', at most 28 digits).
func ParseDecimal(s string) (Decimal, error) {
	digits, seenDot := 0, false
	start := 0
	if len(s) > 0 && s[0] == '-' {
		start = 1
	}
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits++
		case c == '.' && !seenDot && i > start && i < len(s)-1:
			seenDot = true
		default:
			return Decimal{}, fmt.Errorf("erp: invalid decimal %q", s)
		}
	}
	if digits == 0 || digits > maxDecimalDigits {
		return Decimal{}, fmt.Errorf("erp: invalid decimal %q", s)
	}
	var out Decimal
	if _, _, err := out.d.SetString(s); err != nil {
		return Decimal{}, fmt.Errorf("erp: invalid decimal %q: %w", s, err)
	}
	return out, nil
}

// MustDecimal is ParseDecimal that panics; for constants and tests.
func MustDecimal(s string) Decimal {
	d, err := ParseDecimal(s)
	if err != nil {
		panic(err)
	}
	return d
}

func (x Decimal) String() string { return x.d.Text('f') }

// Cmp returns -1, 0 or 1.
func (x Decimal) Cmp(y Decimal) int { return x.d.Cmp(&y.d) }

func (x Decimal) IsZero() bool { return x.d.IsZero() }

func must(err error) {
	if err != nil {
		panic(fmt.Errorf("erp: decimal arithmetic: %w", err))
	}
}

// Add returns x+y. It panics only on exponent overflow, unreachable for parsed values.
func (x Decimal) Add(y Decimal) Decimal {
	var out Decimal
	_, err := arithCtx.Add(&out.d, &x.d, &y.d)
	must(err)
	return out
}

// Sub returns x-y.
func (x Decimal) Sub(y Decimal) Decimal {
	var out Decimal
	_, err := arithCtx.Sub(&out.d, &x.d, &y.d)
	must(err)
	return out
}

// Mul returns x*y.
func (x Decimal) Mul(y Decimal) Decimal {
	var out Decimal
	_, err := arithCtx.Mul(&out.d, &x.d, &y.d)
	must(err)
	return out
}

// RoundTo rounds half away from zero to the given number of decimal places.
func (x Decimal) RoundTo(places int32) Decimal {
	var out Decimal
	_, err := arithCtx.Quantize(&out.d, &x.d, -places)
	must(err)
	return out
}

// MarshalJSON encodes as a JSON string.
func (x Decimal) MarshalJSON() ([]byte, error) { return json.Marshal(x.String()) }

// UnmarshalJSON accepts a JSON string or a bare JSON number (kept as text, never float64).
func (x *Decimal) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	var d Decimal
	var err error
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err = json.Unmarshal(b, &s); err != nil {
			return err
		}
		d, err = ParseDecimal(s)
	} else {
		d, err = parseJSONNumber(string(b))
	}
	if err != nil {
		return err
	}
	*x = d
	return nil
}

// parseJSONNumber reads a bare JSON number, including exponent forms such as
// 1e-05 that Python json.dumps writes, and normalises it to plain notation.
func parseJSONNumber(s string) (Decimal, error) {
	if s == "" || (s[0] != '-' && (s[0] < '0' || s[0] > '9')) || !json.Valid([]byte(s)) {
		return Decimal{}, fmt.Errorf("erp: invalid decimal %q", s)
	}
	var tmp apd.Decimal
	if _, _, err := tmp.SetString(s); err != nil || tmp.Form != apd.Finite {
		return Decimal{}, fmt.Errorf("erp: invalid decimal %q", s)
	}
	if tmp.Exponent > maxDecimalDigits || tmp.Exponent < -maxDecimalDigits {
		return Decimal{}, fmt.Errorf("erp: decimal %q out of range", s)
	}
	return ParseDecimal(tmp.Text('f'))
}
