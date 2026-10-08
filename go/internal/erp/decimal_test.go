package erp_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

func TestParseDecimalExact(t *testing.T) {
	for _, s := range []string{"1234567.89", "0.1", "135.0", "0", "-42.50"} {
		d, err := erp.ParseDecimal(s)
		require.NoError(t, err, s)
		assert.Equal(t, s, d.String())
	}
	d := erp.MustDecimal("0.1")
	b, err := json.Marshal(d)
	require.NoError(t, err)
	assert.JSONEq(t, `"0.1"`, string(b))

	var back erp.Decimal
	require.NoError(t, json.Unmarshal([]byte(`"0.1"`), &back))
	assert.Equal(t, "0.1", back.String())
	// ERPNext-style bare JSON numbers decode exactly too.
	require.NoError(t, json.Unmarshal([]byte(`135.0`), &back))
	assert.Equal(t, "135.0", back.String())
}

func TestParseDecimalRejects(t *testing.T) {
	for _, s := range []string{"", "abc", "NaN", "Inf", "1e5", "1.", ".5", "--1", "1 ", "+1", "12345678901234567890123456789"} {
		_, err := erp.ParseDecimal(s)
		assert.Error(t, err, s)
	}
}

func TestDecimalRoundTo(t *testing.T) {
	assert.Equal(t, "8.41", erp.MustDecimal("8.405").RoundTo(2).String())
	assert.Equal(t, "-8.41", erp.MustDecimal("-8.405").RoundTo(2).String())
	assert.Equal(t, "2.52", erp.MustDecimal("36").Mul(erp.MustDecimal("0.07")).RoundTo(2).String())
}

func TestDecimalArithmetic(t *testing.T) {
	assert.Equal(t, "0.3", erp.MustDecimal("0.1").Add(erp.MustDecimal("0.2")).String())
	assert.Equal(t, 0, erp.MustDecimal("1.50").Cmp(erp.MustDecimal("1.5")))
	assert.Negative(t, erp.MustDecimal("1").Cmp(erp.MustDecimal("2")))
}

func genDecimal() *rapid.Generator[erp.Decimal] {
	return rapid.Custom(func(t *rapid.T) erp.Decimal {
		m := rapid.Int64Range(-999_999_999_999, 999_999_999_999).Draw(t, "mantissa")
		scale := rapid.IntRange(0, 6).Draw(t, "scale")
		s := fmt.Sprintf("%d", m)
		if scale > 0 {
			neg := ""
			if m < 0 {
				neg, m = "-", -m
			}
			s = fmt.Sprintf("%s%d.%0*d", neg, m/pow10(scale), scale, m%pow10(scale))
		}
		return erp.MustDecimal(s)
	})
}

func pow10(n int) int64 {
	p := int64(1)
	for range n {
		p *= 10
	}
	return p
}

func TestDecimalProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b, c := genDecimal().Draw(t, "a"), genDecimal().Draw(t, "b"), genDecimal().Draw(t, "c")
		if a.Add(b).Cmp(b.Add(a)) != 0 {
			t.Fatalf("add not commutative: %s %s", a, b)
		}
		if a.Add(b).Add(c).Cmp(a.Add(b.Add(c))) != 0 {
			t.Fatalf("add not associative: %s %s %s", a, b, c)
		}
		r := a.RoundTo(2)
		if r.RoundTo(2).String() != r.String() {
			t.Fatalf("RoundTo(2) not idempotent: %s", a)
		}
		p, err := erp.ParseDecimal(a.String())
		if err != nil || p.String() != a.String() || p.Cmp(a) != 0 {
			t.Fatalf("string round-trip failed: %s (%v)", a, err)
		}
		raw, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		var j erp.Decimal
		if err := json.Unmarshal(raw, &j); err != nil || j.String() != a.String() {
			t.Fatalf("json round-trip failed: %s (%v)", raw, err)
		}
	})
}

func FuzzParseDecimal(f *testing.F) {
	for _, s := range []string{"1234567.89", "0.1", "135.0", "-0.5", "", "1e9", "NaN", "00.10"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := erp.ParseDecimal(s)
		if err != nil {
			return
		}
		back, err := erp.ParseDecimal(d.String())
		if err != nil {
			t.Fatalf("accepted %q but its String %q does not parse: %v", s, d.String(), err)
		}
		if back.String() != d.String() || back.Cmp(d) != 0 {
			t.Fatalf("round-trip mismatch for %q: %q vs %q", s, back, d)
		}
	})
}
