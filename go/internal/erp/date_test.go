package erp_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

func TestParseDateRejectsBad(t *testing.T) {
	for _, s := range []string{"2026-13-01", "2026-02-30", "2026-1-2", "", "20261009", "2026-10-09T00:00:00Z", " 2026-10-09"} {
		_, err := erp.ParseDate(s)
		assert.Error(t, err, s)
	}
	d, err := erp.ParseDate("2026-10-09")
	require.NoError(t, err)
	assert.Equal(t, "2026-10-09", d.String())
	assert.Equal(t, erp.Date{Year: 2026, Month: time.October, Day: 9}, d)
}

func TestDateJSONAndAddDays(t *testing.T) {
	d := erp.MustDate("2026-12-30")
	b, err := json.Marshal(d)
	require.NoError(t, err)
	assert.JSONEq(t, `"2026-12-30"`, string(b))
	var back erp.Date
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, d, back)
	assert.Equal(t, "2027-01-06", d.AddDays(7).String())
	assert.True(t, erp.Date{}.IsZero())
}

func TestZeroDateJSON(t *testing.T) {
	b, err := json.Marshal(erp.Date{})
	require.NoError(t, err)
	assert.Equal(t, "null", string(b))
	keep := erp.MustDate("2026-10-09")
	require.NoError(t, json.Unmarshal(b, &keep))
	assert.Equal(t, "2026-10-09", keep.String(), "null is a no-op")
	var z erp.Date
	require.NoError(t, json.Unmarshal([]byte(`""`), &z))
	assert.True(t, z.IsZero())

	type doc struct {
		PostingDate erp.Date
		DueDate     erp.Date
		Amount      erp.Decimal
	}
	in := doc{PostingDate: erp.MustDate("2026-10-09"), Amount: erp.MustDecimal("1.5")}
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	var out doc
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Equal(t, in.PostingDate, out.PostingDate)
	assert.True(t, out.DueDate.IsZero())
	assert.Equal(t, "1.5", out.Amount.String())
}

func FuzzParseDate(f *testing.F) {
	for _, s := range []string{"2026-10-09", "2026-13-01", "0000-01-01", "9999-12-31", "2026-02-29", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := erp.ParseDate(s)
		if err != nil {
			return
		}
		if d.String() != s {
			t.Fatalf("accepted %q but String is %q", s, d.String())
		}
	})
}
