package gateway_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/erpfake"
	"github.com/atipongsena/erp-town/go/internal/gateway"
	"github.com/atipongsena/erp-town/go/internal/platform"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

var testSeed = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))

type harness struct {
	t   *testing.T
	h   http.Handler
	jwt *auth.JWTIssuer
}

func newHarness(t *testing.T, port erp.Port) *harness {
	t.Helper()
	jwt, err := auth.NewJWTIssuer(testSeed, 15*time.Minute)
	require.NoError(t, err)
	mux := platform.NewMux()
	gateway.New(mux, port, auth.NewAuthenticator(port, 30*time.Second), jwt)
	return &harness{t: t, h: mux, jwt: jwt}
}

type reqOpt func(*http.Request)

func withSession(sid string) reqOpt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "sid", Value: sid}) }
}

func withTownHeader() reqOpt {
	return func(r *http.Request) { r.Header.Set("X-Town-Request", "1") }
}

func (h *harness) do(method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	h.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	r := httptest.NewRequestWithContext(h.t.Context(), method, path, rd)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	h.h.ServeHTTP(w, r)
	return w
}

type errBody struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields"`
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v), "body: %s", w.Body.String())
	return v
}

const salesSession = "sess-sales"

const orderBody = `{"company":"ACME-TH","customer":"CUST-001","transaction_date":"2026-10-09",` +
	`"delivery_date":"2026-10-16","lines":[{"item_code":"ITEM-002","qty":"2"}]}`

func TestStateChangingRequestNeedsTownHeader(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	for _, sid := range []string{"", salesSession} {
		var opts []reqOpt
		if sid != "" {
			opts = append(opts, withSession(sid))
		}
		w := h.do(http.MethodPost, "/town/api/sales-orders", orderBody, opts...)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		assert.Equal(t, "CSRF_HEADER_MISSING", decode[errBody](t, w).Code)
		assert.NotEmpty(t, w.Header().Get("X-Request-Id"))
	}
	// A wrong value is not the header.
	r := h.do(http.MethodPost, "/town/api/auth/token", "", withSession(salesSession),
		func(r *http.Request) { r.Header.Set("X-Town-Request", "yes") })
	require.Equal(t, http.StatusForbidden, r.Code)
	assert.Equal(t, "CSRF_HEADER_MISSING", decode[errBody](t, r).Code)
}

func TestNoSessionIs401(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	for _, opts := range [][]reqOpt{nil, {withSession("Guest")}, {withSession("no-such-session")}} {
		w := h.do(http.MethodGet, "/town/api/me", "", opts...)
		require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		assert.Equal(t, "UNAUTHENTICATED", decode[errBody](t, w).Code)
	}
	// The town header does not replace a session.
	w := h.do(http.MethodPost, "/town/api/sales-orders", orderBody, withTownHeader())
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMeReturnsPrincipal(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	w := h.do(http.MethodGet, "/town/api/me", "", withSession(salesSession))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"user":"sales@acme-th.test","roles":["Sales User"],"companies":["ACME-TH"]}`, w.Body.String())
}

func TestCreateGetSubmitSalesOrder(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	w := h.do(http.MethodPost, "/town/api/sales-orders", orderBody, withSession(salesSession), withTownHeader())
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	name := decode[struct{ Name string }](t, w).Name
	require.NotEmpty(t, name)

	w = h.do(http.MethodGet, "/town/api/sales-orders/"+name, "", withSession(salesSession))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var so map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &so))
	assert.Equal(t, name, so["name"])
	assert.Equal(t, "ACME-TH", so["company"])
	assert.Equal(t, "CUST-001", so["customer"])
	assert.Equal(t, "2026-10-09", so["transaction_date"])
	assert.Equal(t, "2026-10-16", so["delivery_date"])
	assert.Equal(t, "24", so["net_total"])
	assert.Equal(t, "1.68", so["tax_total"])
	assert.Equal(t, "25.68", so["grand_total"])
	assert.Equal(t, "26", so["rounded_total"])
	assert.Equal(t, "Draft", so["status"])
	assert.Equal(t, []any{map[string]any{"item_code": "ITEM-002", "qty": "2", "rate": "12", "amount": "24"}}, so["lines"])

	w = h.do(http.MethodPost, "/town/api/documents/sales-order/"+name+"/submit", "",
		withSession(salesSession), withTownHeader())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	sub := decode[map[string]any](t, w)
	assert.Equal(t, true, sub["submitted"])
	assert.NotEmpty(t, sub["workflow_state"])

	// Unknown doctype in the path is a validation failure.
	w = h.do(http.MethodPost, "/town/api/documents/item/"+name+"/submit", "",
		withSession(salesSession), withTownHeader())
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, decode[errBody](t, w).Fields, "path.doctype")
}

// stubPort overrides CreateSalesOrder on top of the fake (whoami still works).
type stubPort struct {
	erp.Port
	err   error
	calls atomic.Int32
}

func (s *stubPort) CreateSalesOrder(context.Context, erp.Principal, erp.SalesOrderInput) (erp.DocRef, error) {
	s.calls.Add(1)
	return erp.DocRef{}, s.err
}

func TestDomainErrorsMapToHTTP(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{erp.ErrUnauthenticated, 401, "UNAUTHENTICATED"},
		{fmt.Errorf("%w: no access", erp.ErrForbidden), 403, "FORBIDDEN"},
		{fmt.Errorf("%w: Customer CUST-X", erp.ErrNotFound), 404, "NOT_FOUND"},
		{fmt.Errorf("%w: modified", erp.ErrConflict), 409, "CONFLICT"},
		{fmt.Errorf("wrapped: %w", &erp.ValidationError{Message: "bad customer", Fields: map[string]string{"customer": "unknown"}}), 422, "VALIDATION"},
		{fmt.Errorf("%w: dial tcp 10.0.0.1:8080", erp.ErrUnavailable), 503, "UNAVAILABLE"},
		{errors.New("secret internal detail"), 500, "INTERNAL"},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			h := newHarness(t, &stubPort{Port: erpfake.DefaultEnv().Port, err: c.err})
			w := h.do(http.MethodPost, "/town/api/sales-orders", orderBody, withSession(salesSession), withTownHeader())
			require.Equal(t, c.status, w.Code, w.Body.String())
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			body := decode[errBody](t, w)
			assert.Equal(t, c.code, body.Code)
			assert.NotEmpty(t, body.Message)
			assert.NotContains(t, w.Body.String(), "secret internal detail")
			assert.NotContains(t, w.Body.String(), "10.0.0.1")
			if c.code == "VALIDATION" {
				assert.Equal(t, map[string]string{"customer": "unknown"}, body.Fields)
				assert.Equal(t, "bad customer", body.Message)
			}
			if c.code == "INTERNAL" {
				assert.Equal(t, "internal error", body.Message)
			}
			assert.NotEmpty(t, w.Header().Get("X-Request-Id"))
		})
	}
}

func TestUnavailableIs503AndNotRetried(t *testing.T) {
	stub := &stubPort{Port: erpfake.DefaultEnv().Port, err: fmt.Errorf("%w: timeout", erp.ErrUnavailable)}
	h := newHarness(t, stub)
	w := h.do(http.MethodPost, "/town/api/sales-orders", orderBody, withSession(salesSession), withTownHeader())
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	assert.Equal(t, "UNAVAILABLE", decode[errBody](t, w).Code)
	assert.Equal(t, int32(1), stub.calls.Load())
}

func TestTokenEndpointIssuesVerifiableJWT(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	w := h.do(http.MethodPost, "/town/api/auth/token", "", withSession(salesSession), withTownHeader())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := decode[struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}](t, w)
	claims, err := h.jwt.Verify(body.Token)
	require.NoError(t, err)
	assert.Equal(t, "sales@acme-th.test", claims.Subject)
	assert.Equal(t, []string{"Sales User"}, claims.Roles)
	assert.Equal(t, []string{"ACME-TH"}, claims.Companies)
	assert.True(t, claims.ExpiresAt.Equal(body.ExpiresAt), "%v vs %v", claims.ExpiresAt, body.ExpiresAt)
}

func TestMalformedBodyIs422(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	bad := strings.Replace(orderBody, `"qty":"2"`, `"qty":"abc"`, 1)
	w := h.do(http.MethodPost, "/town/api/sales-orders", bad, withSession(salesSession), withTownHeader())
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	body := decode[errBody](t, w)
	assert.Equal(t, "VALIDATION", body.Code)
	assert.Contains(t, body.Fields, "body.lines[0].qty")

	// Syntactically broken JSON is a validation failure too, not a bare 400.
	w = h.do(http.MethodPost, "/town/api/sales-orders", `{"company":`, withSession(salesSession), withTownHeader())
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Equal(t, "VALIDATION", decode[errBody](t, w).Code)

	// Lists are never null.
	noLines := strings.Replace(orderBody, `"lines":[{"item_code":"ITEM-002","qty":"2"}]`, `"lines":null`, 1)
	w = h.do(http.MethodPost, "/town/api/sales-orders", noLines, withSession(salesSession), withTownHeader())
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	assert.Contains(t, decode[errBody](t, w).Fields, "body.lines")
}

func TestOpenAPIDocumentsEveryRoute(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	w := h.do(http.MethodGet, "/town/api/openapi.json", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var doc struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Title string `json:"title"`
		} `json:"info"`
		Paths      map[string]map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
				Required   []string       `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	assert.Equal(t, "3.1.0", doc.OpenAPI)
	assert.Equal(t, "ERP Town API", doc.Info.Title)

	wantOps := map[string]string{
		"get /town/api/me":                                 "get-me",
		"post /town/api/auth/token":                        "issue-token",
		"post /town/api/sales-orders":                      "create-sales-order",
		"get /town/api/sales-orders/{name}":                "get-sales-order",
		"post /town/api/documents/{doctype}/{name}/submit": "submit-document",
	}
	for key, opID := range wantOps {
		method, path, _ := strings.Cut(key, " ")
		op, ok := doc.Paths[path][method]
		require.True(t, ok, "missing %s", key)
		assert.Equal(t, opID, op["operationId"])
		responses, _ := op["responses"].(map[string]any)
		require.Contains(t, responses, "401", "%s documents 401", key)
		for status, resp := range responses {
			if status[0] < '4' {
				continue
			}
			ref := resp.(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
			assert.Equal(t, "#/components/schemas/APIError", ref, "%s %s", key, status)
		}
	}
	apiErr, ok := doc.Components.Schemas["APIError"]
	require.True(t, ok)
	assert.Contains(t, apiErr.Properties, "code")
	assert.Contains(t, apiErr.Properties, "message")
	assert.Contains(t, apiErr.Properties, "fields")
	assert.ElementsMatch(t, []string{"code", "message"}, apiErr.Required)
}

func TestDocsPageServed(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	w := h.do(http.MethodGet, "/town/api/docs", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, strings.HasPrefix(w.Header().Get("Content-Type"), "text/html"))
	assert.Contains(t, w.Body.String(), "/town/api/openapi")
}

func TestHealthzStillServed(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	w := h.do(http.MethodGet, "/town/api/healthz", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"ok"}`, w.Body.String())
}

// zeroDatePort returns a Sales Order without a delivery date (Frappe null).
type zeroDatePort struct{ erp.Port }

func (zeroDatePort) GetSalesOrder(_ context.Context, _ erp.Principal, name string) (erp.SalesOrder, error) {
	return erp.SalesOrder{
		Ref: erp.DocRef{DocType: erp.DocSalesOrder, Name: name}, Company: "ACME-TH", Customer: "CUST-001",
		TransactionDate: erp.MustDate("2026-10-09"), Status: erp.DocDraft, WorkflowState: "Draft",
	}, nil
}

func TestZeroDateIsNull(t *testing.T) {
	h := newHarness(t, zeroDatePort{erpfake.DefaultEnv().Port})
	w := h.do(http.MethodGet, "/town/api/sales-orders/SAL-ORD-00009", "", withSession(salesSession))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	so := decode[map[string]any](t, w)
	require.Contains(t, so, "delivery_date")
	assert.Nil(t, so["delivery_date"])
	assert.Equal(t, "2026-10-09", so["transaction_date"])

	// The published schema says so too.
	w = h.do(http.MethodGet, "/town/api/openapi.json", "")
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type   any    `json:"type"`
					Format string `json:"format"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	for _, f := range []string{"transaction_date", "delivery_date"} {
		p := doc.Components.Schemas["SalesOrderOutputBody"].Properties[f]
		assert.Equal(t, []any{"string", "null"}, p.Type, f)
		assert.Equal(t, "date", p.Format, f)
	}
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/town/api/nope"},
		{http.MethodPut, "/town/api/sales-orders"},
		{http.MethodDelete, "/town/api/me"},
	} {
		w := h.do(c.method, c.path, "", withSession(salesSession), withTownHeader())
		require.Equal(t, http.StatusNotFound, w.Code, "%s %s: %s", c.method, c.path, w.Body.String())
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
		assert.Equal(t, "NOT_FOUND", decode[errBody](t, w).Code)
	}
	// The catch-all shadows neither the operations nor their CSRF and auth checks.
	assert.Equal(t, http.StatusForbidden, h.do(http.MethodPost, "/town/api/sales-orders", orderBody, withSession(salesSession)).Code)
	assert.Equal(t, http.StatusUnauthorized, h.do(http.MethodGet, "/town/api/me", "").Code)
	assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/town/api/me", "", withSession(salesSession)).Code)
}

func TestAPIDescriptionMentionsValidationScope(t *testing.T) {
	h := newHarness(t, erpfake.DefaultEnv().Port)
	var doc struct {
		Info struct {
			Description string `json:"description"`
		} `json:"info"`
	}
	require.NoError(t, json.Unmarshal(h.do(http.MethodGet, "/town/api/openapi.json", "").Body.Bytes(), &doc))
	for _, s := range []string{"too large", "content type", "timeout"} {
		assert.Contains(t, doc.Info.Description, s)
	}
}

// blockingPort blocks CreateSalesOrder until its context ends and returns the
// context error the way the ERPNext client wraps it. onCall, when set, runs
// first (e.g. to simulate the client going away).
type blockingPort struct {
	erp.Port
	onCall func()
}

func (b *blockingPort) CreateSalesOrder(ctx context.Context, _ erp.Principal, _ erp.SalesOrderInput) (erp.DocRef, error) {
	if b.onCall != nil {
		b.onCall()
	}
	<-ctx.Done()
	return erp.DocRef{}, fmt.Errorf("erpnext: request aborted: %w", ctx.Err())
}

func TestRequestDeadlineIs503(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, &blockingPort{Port: erpfake.DefaultEnv().Port})
		start := time.Now()
		w := h.do(http.MethodPost, "/town/api/sales-orders", orderBody, withSession(salesSession), withTownHeader())
		assert.Equal(t, 45*time.Second, time.Since(start))
		require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
		assert.Equal(t, "UNAVAILABLE", decode[errBody](t, w).Code)
	})
}

func TestClientDisconnectIsNotAnError(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h := newHarness(t, &blockingPort{Port: erpfake.DefaultEnv().Port, onCall: cancel})
	w := h.do(http.MethodPost, "/town/api/sales-orders", orderBody, withSession(salesSession), withTownHeader(),
		func(r *http.Request) { *r = *r.WithContext(ctx) })

	assert.NotEqual(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "INTERNAL")
	assert.NotContains(t, logs.String(), `"level":"ERROR"`)
	assert.Contains(t, logs.String(), "client closed")
}
