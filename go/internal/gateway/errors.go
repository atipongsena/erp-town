package gateway

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// Error codes of the gateway's error body.
const (
	CodeUnauthenticated   = "UNAUTHENTICATED"
	CodeForbidden         = "FORBIDDEN"
	CodeNotFound          = "NOT_FOUND"
	CodeConflict          = "CONFLICT"
	CodeValidation        = "VALIDATION"
	CodeUnavailable       = "UNAVAILABLE"
	CodeInternal          = "INTERNAL"
	CodeCSRFHeaderMissing = "CSRF_HEADER_MISSING"
)

const internalMessage = "internal error"

// APIError is the body of every gateway error response:
// {"code","message","fields"?}. Status is the HTTP status and is not encoded.
type APIError struct {
	Status  int               `json:"-"`
	Code    string            `json:"code" enum:"UNAUTHENTICATED,FORBIDDEN,NOT_FOUND,CONFLICT,VALIDATION,UNAVAILABLE,INTERNAL,CSRF_HEADER_MISSING" doc:"Machine-readable error code"`
	Message string            `json:"message" doc:"Human-readable explanation"`
	Fields  map[string]string `json:"fields,omitempty" doc:"Per-field reasons, keyed by field or request location (e.g. body.lines[0].qty)"`
}

var _ huma.StatusError = (*APIError)(nil)

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// GetStatus implements huma.StatusError.
func (e *APIError) GetStatus() int { return e.Status }

// toAPIError maps a Port or gateway error to its response. Domain messages
// that come from the ERP (validation, not found, forbidden, conflict) are
// passed on; anything unexpected becomes 500 INTERNAL "internal error", and
// 401/503 use fixed messages so transport details never reach clients.
func toAPIError(err error) *APIError {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae
	}
	var ve *erp.ValidationError
	switch {
	case errors.As(err, &ve):
		msg := ve.Message
		if msg == "" {
			msg = "validation failed"
		}
		return &APIError{Status: http.StatusUnprocessableEntity, Code: CodeValidation, Message: msg, Fields: maps.Clone(ve.Fields)}
	case errors.Is(err, erp.ErrValidation):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: CodeValidation, Message: "validation failed"}
	case errors.Is(err, erp.ErrUnauthenticated):
		return &APIError{Status: http.StatusUnauthorized, Code: CodeUnauthenticated, Message: "authentication required"}
	case errors.Is(err, erp.ErrForbidden):
		return &APIError{Status: http.StatusForbidden, Code: CodeForbidden, Message: domainMessage(err, erp.ErrForbidden)}
	case errors.Is(err, erp.ErrNotFound):
		return &APIError{Status: http.StatusNotFound, Code: CodeNotFound, Message: domainMessage(err, erp.ErrNotFound)}
	case errors.Is(err, erp.ErrConflict):
		return &APIError{Status: http.StatusConflict, Code: CodeConflict, Message: domainMessage(err, erp.ErrConflict)}
	case errors.Is(err, erp.ErrUnavailable):
		return &APIError{Status: http.StatusServiceUnavailable, Code: CodeUnavailable, Message: "ERP is unavailable, try again later"}
	}
	return &APIError{Status: http.StatusInternalServerError, Code: CodeInternal, Message: internalMessage}
}

// domainMessage strips the "erp: " prefix of the sentinel so clients read
// e.g. "not found: Sales Order SAL-ORD-00001".
func domainMessage(err, sentinel error) string {
	msg := strings.TrimPrefix(err.Error(), "erp: ")
	if msg == "" {
		return strings.TrimPrefix(sentinel.Error(), "erp: ")
	}
	return msg
}

// fail maps err for a handler or middleware, logging server-side failures
// with the request id.
func fail(ctx context.Context, err error) *APIError {
	ae := toAPIError(err)
	logServerError(ctx, ae, err)
	return ae
}

func logServerError(ctx context.Context, ae *APIError, cause error) {
	if ae.Status < http.StatusInternalServerError {
		return
	}
	slog.ErrorContext(ctx, "gateway: request failed",
		"request_id", requestID(ctx), "status", ae.Status, "code", ae.Code, "err", cause)
}

// humaError turns Huma's own failures (body/param validation, unparsable
// JSON, content negotiation) into an APIError. Every client-side failure is
// 422 VALIDATION with fields keyed by Huma's location (e.g.
// body.lines[0].qty); auth-like statuses keep their codes; any server-side
// status is 500 INTERNAL.
func humaError(status int, msg string, errs ...error) *APIError {
	fields := map[string]string{}
	for _, e := range errs {
		if e == nil {
			continue
		}
		loc, text := "", e.Error()
		var d huma.ErrorDetailer
		if errors.As(e, &d) {
			if detail := d.ErrorDetail(); detail != nil {
				loc, text = detail.Location, detail.Message
			}
		}
		if loc == "" {
			loc = "request"
		}
		if prev, ok := fields[loc]; ok {
			text = prev + "; " + text
		}
		fields[loc] = text
	}
	if len(fields) == 0 {
		fields = nil
	}
	switch {
	case status == http.StatusUnauthorized:
		return &APIError{Status: status, Code: CodeUnauthenticated, Message: msg}
	case status == http.StatusForbidden:
		return &APIError{Status: status, Code: CodeForbidden, Message: msg}
	case status == http.StatusNotFound:
		return &APIError{Status: status, Code: CodeNotFound, Message: msg}
	case status == http.StatusConflict:
		return &APIError{Status: status, Code: CodeConflict, Message: msg}
	case status == http.StatusServiceUnavailable:
		return &APIError{Status: status, Code: CodeUnavailable, Message: "ERP is unavailable, try again later"}
	case status >= http.StatusInternalServerError || status < http.StatusBadRequest:
		// Status 0 is Huma probing the error type for the OpenAPI schema.
		return &APIError{Status: http.StatusInternalServerError, Code: CodeInternal, Message: internalMessage}
	}
	if msg == "" {
		msg = "validation failed"
	}
	return &APIError{Status: http.StatusUnprocessableEntity, Code: CodeValidation, Message: msg, Fields: fields}
}

// configureHuma replaces Huma's problem+json errors with APIError and makes
// arrays non-nullable (our bodies never send null lists). Huma keeps these as
// package variables, so this runs once per process.
//
// WARNING: these overrides are process-wide and apply to every Huma API in
// the binary; the town binary must hold only this one Huma API.
func configureHuma() {
	huma.DefaultArrayNullable = false
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return humaError(status, msg, errs...)
	}
	huma.NewErrorWithContext = func(hctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
		ae := humaError(status, msg, errs...)
		if hctx != nil {
			logServerError(hctx.Context(), ae, errors.Join(append([]error{errors.New(msg)}, errs...)...))
		}
		return ae
	}
}
