package erpnext

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func resp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// recorder is an in-memory RoundTripper that records requests.
type recorder struct {
	mu    sync.Mutex
	reqs  []*http.Request
	times []time.Time
	reply func(n int, r *http.Request) (*http.Response, error)
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.reqs = append(r.reqs, req)
	r.times = append(r.times, time.Now())
	n := len(r.reqs)
	r.mu.Unlock()
	return r.reply(n, req)
}

func newTestClient(rt http.RoundTripper) *Client {
	return NewClient(ClientConfig{BaseURL: "http://erp.test", Site: "test.localhost", Transport: rt})
}

var keyPrincipal = erp.Principal{Credential: erp.APIKeyCredential{Key: "k", Secret: "s"}}

func TestSessionHeaders(t *testing.T) {
	rec := &recorder{reply: func(int, *http.Request) (*http.Response, error) { return resp(200, `{"message":"pong"}`), nil }}
	c := newTestClient(rec)
	p := erp.Principal{Credential: erp.SessionCredential{SessionID: "s1", CSRFToken: "c1"}}
	var out string
	require.NoError(t, c.Call(context.Background(), p, "ping", map[string]any{"a": 1}, &out))
	assert.Equal(t, "pong", out)
	require.Len(t, rec.reqs, 1)
	h := rec.reqs[0].Header
	assert.Equal(t, "sid=s1", h.Get("Cookie"))
	assert.Equal(t, "c1", h.Get("X-Frappe-CSRF-Token"))
	assert.Equal(t, "test.localhost", h.Get("X-Frappe-Site-Name"))
	assert.Equal(t, "application/json", h.Get("Accept"))
	assert.Equal(t, "application/json", h.Get("Content-Type"))
	assert.Empty(t, h.Get("Authorization"))
	assert.Empty(t, h.Get("X-Town-On-Behalf-Of"))
	assert.Equal(t, http.MethodPost, rec.reqs[0].Method)
	assert.Equal(t, "/api/method/ping", rec.reqs[0].URL.Path)
	body, _ := io.ReadAll(rec.reqs[0].Body)
	assert.JSONEq(t, `{"a":1}`, string(body))
}

func TestTokenAndOnBehalfOfHeaders(t *testing.T) {
	rec := &recorder{reply: func(int, *http.Request) (*http.Response, error) { return resp(200, `{"data":{"name":"X"}}`), nil }}
	c := newTestClient(rec)
	p := erp.Principal{
		Credential: erp.APIKeyCredential{Key: "k", Secret: "s"},
		OnBehalfOf: "sales@acme-th.test",
	}
	var out map[string]any
	q := url.Values{"fields": {`["name"]`}}
	require.NoError(t, c.Get(context.Background(), p, "/api/resource/Customer/X", q, &out))
	assert.Equal(t, "X", out["name"])
	h := rec.reqs[0].Header
	assert.Equal(t, "token k:s", h.Get("Authorization"))
	assert.Equal(t, "sales@acme-th.test", h.Get("X-Town-On-Behalf-Of"))
	assert.Equal(t, "test.localhost", h.Get("X-Frappe-Site-Name"))
	assert.Empty(t, h.Get("Cookie"))
	assert.Equal(t, `["name"]`, rec.reqs[0].URL.Query().Get("fields"))
}

func TestCallCredAndMissingCredential(t *testing.T) {
	rec := &recorder{reply: func(int, *http.Request) (*http.Response, error) { return resp(200, `{"message":{"user":"u"}}`), nil }}
	c := newTestClient(rec)
	var out map[string]any
	require.NoError(t, c.CallCred(context.Background(), erp.SessionCredential{SessionID: "s9"}, "erp_town.session_api.whoami", nil, &out))
	assert.Equal(t, "sid=s9", rec.reqs[0].Header.Get("Cookie"))
	err := c.Call(context.Background(), erp.Principal{}, "x", nil, nil)
	require.ErrorIs(t, err, erp.ErrUnauthenticated)
	assert.Len(t, rec.reqs, 1)
}

func TestErrorMapping(t *testing.T) {
	msgs := `{"exc_type":"ValidationError","_server_messages":"[\"{\\\"message\\\":\\\"<b>Customer</b> CUST-NOPE not found\\\"}\"]"}`
	tests := []struct {
		name   string
		status int
		body   string
		target error
	}{
		{"does-not-exist", 404, `{"exc_type":"DoesNotExistError"}`, erp.ErrNotFound},
		{"404 no body", 404, ``, erp.ErrNotFound},
		{"permission", 403, `{"exc_type":"PermissionError"}`, erp.ErrForbidden},
		{"403 plain", 403, `nope`, erp.ErrForbidden},
		{"authentication", 401, `{"exc_type":"AuthenticationError"}`, erp.ErrUnauthenticated},
		{"timestamp", 409, `{"exc_type":"TimestampMismatchError"}`, erp.ErrConflict},
		{"validation 417", 417, msgs, erp.ErrValidation},
		{"link validation", 417, `{"exc_type":"LinkValidationError"}`, erp.ErrValidation},
		{"mandatory", 417, `{"exc_type":"MandatoryError"}`, erp.ErrValidation},
		{"other validation family", 417, `{"exc_type":"InvalidNamingSeriesError"}`, erp.ErrValidation},
		{"exc_type wins over status", 500, `{"exc_type":"DoesNotExistError"}`, erp.ErrNotFound},
		{"417 no exc_type", 417, `{}`, erp.ErrValidation},
		{"500", 500, `{"exc_type":"Exception"}`, erp.ErrUnavailable},
		{"503 html", 503, `<html>down</html>`, erp.ErrUnavailable},
		{"502", 502, ``, erp.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mapError(tt.status, []byte(tt.body))
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.target)
		})
	}

	t.Run("validation message", func(t *testing.T) {
		err := mapError(417, []byte(msgs))
		var ve *erp.ValidationError
		require.ErrorAs(t, err, &ve)
		assert.Equal(t, "Customer CUST-NOPE not found", ve.Message)
	})

	t.Run("unknown is wrapped with status and exc_type", func(t *testing.T) {
		err := mapError(418, []byte(`{"exc_type":"TeapotError"}`))
		require.Error(t, err)
		for _, s := range []error{erp.ErrNotFound, erp.ErrForbidden, erp.ErrUnauthenticated, erp.ErrConflict, erp.ErrUnavailable, erp.ErrValidation} {
			assert.NotErrorIs(t, err, s)
		}
		assert.Contains(t, err.Error(), "418")
		assert.Contains(t, err.Error(), "TeapotError")
	})

	t.Run("end to end through Get", func(t *testing.T) {
		rec := &recorder{reply: func(int, *http.Request) (*http.Response, error) {
			return resp(404, `{"exc_type":"DoesNotExistError"}`), nil
		}}
		err := newTestClient(rec).Get(context.Background(), keyPrincipal, "/api/resource/Customer/NOPE", nil, nil)
		require.ErrorIs(t, err, erp.ErrNotFound)
		assert.Len(t, rec.reqs, 1, "non-unavailable errors are not retried")
	})
}

func TestGetRetriesTwiceThenFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec := &recorder{reply: func(int, *http.Request) (*http.Response, error) { return resp(503, `{}`), nil }}
		c := newTestClient(rec)
		start := time.Now()
		err := c.Get(context.Background(), keyPrincipal, "/api/resource/Item", nil, nil)
		require.ErrorIs(t, err, erp.ErrUnavailable)
		require.Len(t, rec.times, 3)
		assert.Equal(t, time.Duration(0), rec.times[0].Sub(start))
		assert.Equal(t, 200*time.Millisecond, rec.times[1].Sub(start))
		assert.Equal(t, 1000*time.Millisecond, rec.times[2].Sub(start))
	})
}

func TestGetRecoversAfterRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rec := &recorder{reply: func(n int, _ *http.Request) (*http.Response, error) {
			if n < 2 {
				return resp(502, ``), nil
			}
			return resp(200, `{"data":[1]}`), nil
		}}
		var out []json.Number
		err := newTestClient(rec).Get(context.Background(), keyPrincipal, "/api/resource/Item", nil, &out)
		require.NoError(t, err)
		assert.Equal(t, []json.Number{"1"}, out)
		assert.Len(t, rec.reqs, 2)
	})
}

func TestWritesAreNeverRetried(t *testing.T) {
	t.Run("503", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			rec := &recorder{reply: func(int, *http.Request) (*http.Response, error) { return resp(503, `{}`), nil }}
			err := newTestClient(rec).Call(context.Background(), keyPrincipal, "m", nil, nil)
			require.ErrorIs(t, err, erp.ErrUnavailable)
			assert.Len(t, rec.reqs, 1)
		})
	})
	t.Run("hang times out after 10s", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			rec := &recorder{reply: func(_ int, r *http.Request) (*http.Response, error) {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}}
			start := time.Now()
			err := newTestClient(rec).Call(context.Background(), keyPrincipal, "m", nil, nil)
			require.ErrorIs(t, err, erp.ErrUnavailable)
			assert.Equal(t, 10*time.Second, time.Since(start))
			assert.Len(t, rec.reqs, 1)
		})
	})
	t.Run("caller cancel is not unavailable", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			rec := &recorder{reply: func(_ int, r *http.Request) (*http.Response, error) {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}}
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(time.Second, cancel)
			err := newTestClient(rec).Call(ctx, keyPrincipal, "m", nil, nil)
			require.ErrorIs(t, err, context.Canceled)
			assert.NotErrorIs(t, err, erp.ErrUnavailable)
		})
	})
}

func TestConnectionRefusedIsUnavailable(t *testing.T) {
	rt := rtFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("dial tcp: connection refused") })
	err := newTestClient(rt).Call(context.Background(), keyPrincipal, "m", nil, nil)
	require.ErrorIs(t, err, erp.ErrUnavailable)
}

func TestNumbersDecodeExactly(t *testing.T) {
	rt := rtFunc(func(*http.Request) (*http.Response, error) {
		return resp(200, `{"message":{"grand_total":1234567.89}}`), nil
	})
	var out map[string]any
	err := newTestClient(rt).Call(context.Background(), keyPrincipal, "m", nil, &out)
	require.NoError(t, err)
	assert.Equal(t, json.Number("1234567.89"), out["grand_total"])
}

func FuzzMapError(f *testing.F) {
	f.Add(404, `{"exc_type":"DoesNotExistError"}`)
	f.Add(417, `{"exc_type":"ValidationError","_server_messages":"[\"{\\\"message\\\":\\\"<b>x</b>\\\"}\"]"}`)
	f.Add(500, `<html>`)
	f.Add(401, `{"_server_messages":"not json"}`)
	f.Add(418, `{"exc_type":5,"_server_messages":["a"]}`)
	f.Add(400, ``)
	f.Fuzz(func(t *testing.T, status int, body string) {
		err := mapError(status, []byte(body))
		if status >= 400 && err == nil {
			t.Fatalf("status %d returned nil error", status)
		}
	})
}
