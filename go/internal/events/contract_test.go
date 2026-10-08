package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/events/gen"
)

func fixtures(t testing.TB, kind string) map[string][]byte {
	t.Helper()
	dir := "schema/fixtures/" + kind
	entries, err := fs.ReadDir(schemaFS, dir)
	require.NoError(t, err)
	out := make(map[string][]byte, len(entries))
	for _, e := range entries {
		b, err := fs.ReadFile(schemaFS, dir+"/"+e.Name())
		require.NoError(t, err)
		out[e.Name()] = b
	}
	return out
}

func newValidator(t testing.TB) *Validator {
	t.Helper()
	v, err := NewValidator()
	require.NoError(t, err)
	return v
}

func TestValidFixturesPassAndRoundTrip(t *testing.T) {
	v := newValidator(t)
	valid := fixtures(t, "valid")
	require.Len(t, valid, 13)
	for name, raw := range valid {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, v.Validate(raw))
			e, err := Decode(v, raw)
			require.NoError(t, err)
			out, err := Marshal(e)
			require.NoError(t, err)
			assert.JSONEq(t, string(raw), string(out))
			assert.False(t, bytes.HasSuffix(out, []byte("\n")))
		})
	}
}

func TestInvalidFixturesFail(t *testing.T) {
	v := newValidator(t)
	invalid := fixtures(t, "invalid")
	require.Len(t, invalid, 11)
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			err := v.Validate(raw)
			var se *SchemaError
			require.ErrorAs(t, err, &se)
			assert.NotEmpty(t, se.Reasons)
			_, err = Decode(v, raw)
			require.ErrorAs(t, err, &se)
		})
	}
}

func TestDataDecodesForEveryType(t *testing.T) {
	v := newValidator(t)
	for name, raw := range fixtures(t, "valid") {
		t.Run(name, func(t *testing.T) {
			e, err := Decode(v, raw)
			require.NoError(t, err)
			d, ok := gen.NewData(e.Type)
			require.True(t, ok, e.Type)
			dec := json.NewDecoder(bytes.NewReader(e.Data))
			dec.DisallowUnknownFields()
			require.NoError(t, dec.Decode(d))
		})
	}
	_, ok := gen.NewData("nope")
	assert.False(t, ok)
}

func TestDecodeDataGeneric(t *testing.T) {
	v := newValidator(t)
	raw := fixtures(t, "valid")["erp.sales_order.submitted.json"]
	e, err := Decode(v, raw)
	require.NoError(t, err)
	d, err := DecodeData[gen.ErpSalesOrderSubmittedData](e)
	require.NoError(t, err)
	assert.NotEmpty(t, d.Party)
	_, err = DecodeData[gen.ErpSalesOrderSubmittedData](Envelope{Data: json.RawMessage(`{"bogus":1}`)})
	require.Error(t, err)
}

func TestEventTypesMatchSchema(t *testing.T) {
	raw, err := fs.ReadFile(schemaFS, "schema/envelope.json")
	require.NoError(t, err)
	var env struct {
		Properties struct {
			Type struct {
				Enum []string `json:"enum"`
			} `json:"type"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	got := append([]string(nil), gen.EventTypes...)
	want := env.Properties.Type.Enum
	sort.Strings(got)
	sort.Strings(want)
	assert.Equal(t, want, got)
	assert.Len(t, got, 13)
}

func TestEmbeddedSchemaMatchesSource(t *testing.T) {
	src := filepath.Join("..", "..", "..", "packages", "event-schema")
	seen := 0
	for _, sub := range []string{"schema", "fixtures"} {
		root := filepath.Join(src, sub)
		require.NoError(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			embedded := "schema/" + filepath.ToSlash(rel)
			if sub == "fixtures" {
				embedded = "schema/fixtures/" + filepath.ToSlash(rel)
			}
			want, err := os.ReadFile(p)
			require.NoError(t, err)
			got, err := fs.ReadFile(schemaFS, embedded)
			require.NoError(t, err, embedded)
			assert.Equal(t, strings.ReplaceAll(string(want), "\r\n", "\n"), string(got), embedded)
			seen++
			return nil
		}))
	}
	embedded := 0
	require.NoError(t, fs.WalkDir(schemaFS, "schema", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && p != "schema/README.md" {
			embedded++
		}
		return err
	}))
	assert.Equal(t, seen, embedded, "embedded copy has extra or missing files")
}

func TestMarshalKeepsThaiAndAmpersand(t *testing.T) {
	v := newValidator(t)
	raw := bytes.Replace(fixtures(t, "valid")["system.error.json"],
		[]byte("data must NOT have additional properties (bogus)"),
		[]byte("ส่งไม่สำเร็จ & ลองใหม่ <1>"), 1)
	e, err := Decode(v, raw)
	require.NoError(t, err)
	out, err := Marshal(e)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"ส่งไม่สำเร็จ & ลองใหม่ <1>"`)
}

func TestVisibilityRolesNeverNull(t *testing.T) {
	out, err := Marshal(Envelope{})
	require.NoError(t, err)
	assert.Contains(t, string(out), `"roles":[]`)
	assert.NotContains(t, string(out), `"roles":null`)
}

func FuzzDecode(f *testing.F) {
	v := newValidator(f)
	for _, kind := range []string{"valid", "invalid"} {
		for _, raw := range fixtures(f, kind) {
			f.Add(raw)
		}
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		e, err := Decode(v, raw)
		if err != nil {
			var se *SchemaError
			if errors.As(err, &se) {
				assert.NotEmpty(t, se.Reasons)
			}
			return
		}
		out, err := Marshal(e)
		require.NoError(t, err)
		e2, err := Decode(v, out)
		require.NoError(t, err)
		assert.Equal(t, e.ID, e2.ID)
		assert.Equal(t, e.Type, e2.Type)
		assert.Equal(t, e.Visibility, e2.Visibility)
		assert.JSONEq(t, string(e.Data), string(e2.Data))
	})
}
