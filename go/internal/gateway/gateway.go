// Package gateway is the town's authenticated HTTP API over erp.Port (Huma v2
// on the platform ServeMux). Every operation acts as the caller: the
// Principal resolved from the Frappe session is passed to each Port call.
package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/oklog/ulid/v2"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/platform"
)

const (
	apiTitle   = "ERP Town API"
	apiVersion = "0.1.0"

	// TownRequestHeader must be "1" on every state-changing request (CSRF
	// defence: a cross-site form cannot set it).
	TownRequestHeader = "X-Town-Request"
	// RequestIDHeader carries the per-request id that server logs use.
	RequestIDHeader = "X-Request-Id"

	sessionScheme = "frappeSession"

	// requestTimeout bounds one operation, ERP calls included (a create can
	// chain several of them); hitting it answers 503 UNAVAILABLE.
	requestTimeout = 45 * time.Second
)

var humaOnce sync.Once

type ctxKey int

const principalKey ctxKey = 0

type gateway struct {
	port erp.Port
	jwt  *auth.JWTIssuer
}

// New registers the API on mux: the operations under /town/api/, the OpenAPI
// 3.1 document at GET /town/api/openapi.json and the reference page at GET
// /town/api/docs. Every operation needs a Frappe session (cookie sid);
// POST/PUT/PATCH/DELETE also need header X-Town-Request: 1.
func New(mux *http.ServeMux, port erp.Port, authn *auth.Authenticator, jwt *auth.JWTIssuer) huma.API {
	humaOnce.Do(configureHuma)

	cfg := huma.DefaultConfig(apiTitle, apiVersion)
	cfg.OpenAPIPath = "/town/api/openapi"
	cfg.DocsPath = "/town/api/docs"
	cfg.SchemasPath = ""
	cfg.CreateHooks = nil // no $schema links in response bodies
	cfg.Info.Description = "Authenticated API of ERP Town. Sign in to the ERP first: every operation " +
		"uses the Frappe session cookie `sid`. State-changing requests must send header `" +
		TownRequestHeader + ": 1`. Amounts and quantities are decimal strings. 422 VALIDATION also covers " +
		"a request body that is too large, an unsupported content type, or a body read timeout."
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		sessionScheme: {Type: "apiKey", In: "cookie", Name: "sid", Description: "Frappe session cookie"},
	}
	cfg.Security = []map[string][]string{{sessionScheme: {}}}

	// Unmatched paths and methods under /town/api/ get our JSON 404 instead of
	// net/http's plain-text 404/405; every registered pattern is more specific.
	mux.HandleFunc("/town/api/", notFound)

	api := humago.New(mux, cfg)
	api.UseMiddleware(withRequestID, withAccessLog, withDeadline, requireTownHeader, authenticate(authn))

	g := &gateway{port: port, jwt: jwt}
	g.registerIdentity(api)
	g.registerSales(api)
	return api
}

func withRequestID(ctx huma.Context, next func(huma.Context)) {
	id := ulid.Make().String()
	ctx.SetHeader(RequestIDHeader, id)
	next(huma.WithContext(ctx, platform.WithRequestID(ctx.Context(), id)))
}

func withAccessLog(ctx huma.Context, next func(huma.Context)) {
	start := time.Now()
	defer func() {
		status := ctx.Status()
		if status == 0 {
			status = http.StatusOK
		}
		logAccess(ctx.Context(), ctx.Method(), ctx.URL().Path, status, start)
	}()
	next(ctx)
}

func logAccess(ctx context.Context, method, path string, status int, start time.Time) {
	slog.InfoContext(ctx, "gateway: request completed",
		"request_id", requestID(ctx), "method", method, "path", path,
		"status", status, "duration_ms", float64(time.Since(start))/float64(time.Millisecond))
}

func withDeadline(ctx huma.Context, next func(huma.Context)) {
	dctx, cancel := context.WithTimeout(ctx.Context(), requestTimeout)
	defer cancel()
	next(huma.WithContext(ctx, dctx))
}

func requireTownHeader(ctx huma.Context, next func(huma.Context)) {
	switch ctx.Method() {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		if ctx.Header(TownRequestHeader) != "1" {
			writeError(ctx, &APIError{
				Status: http.StatusForbidden, Code: CodeCSRFHeaderMissing,
				Message: "state-changing requests need header " + TownRequestHeader + ": 1",
			})
			return
		}
	}
	next(ctx)
}

func authenticate(authn *auth.Authenticator) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		r, _ := humago.Unwrap(ctx)
		p, err := authn.FromRequest(r.WithContext(ctx.Context()))
		if err != nil {
			writeError(ctx, fail(ctx.Context(), err))
			return
		}
		next(huma.WithValue(ctx, principalKey, p))
	}
}

// writeError writes e as JSON from middleware, outside Huma's handler path.
func writeError(ctx huma.Context, e *APIError) {
	ctx.SetHeader("Content-Type", "application/json")
	ctx.SetStatus(e.Status)
	enc := json.NewEncoder(ctx.BodyWriter())
	enc.SetEscapeHTML(false)
	_ = enc.Encode(e) // the client went away; nothing else to do
}

func notFound(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := ulid.Make().String()
	ctx := platform.WithRequestID(r.Context(), id)
	defer logAccess(ctx, r.Method, r.URL.Path, http.StatusNotFound, start)
	w.Header().Set(RequestIDHeader, id)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(&APIError{Status: http.StatusNotFound, Code: CodeNotFound, Message: "no such API route"})
}

func requestID(ctx context.Context) string {
	return platform.RequestID(ctx)
}

// principal returns the caller set by the authenticate middleware.
func principal(ctx context.Context) (erp.Principal, error) {
	p, ok := ctx.Value(principalKey).(erp.Principal)
	if !ok {
		return erp.Principal{}, erp.ErrUnauthenticated
	}
	return p, nil
}
