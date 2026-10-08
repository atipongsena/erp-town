package erpnext

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

var htmlTag = regexp.MustCompile(`<[^>]*>`)

// frappeError is the subset of a Frappe error body the mapper reads. Fields
// are RawMessage so a malformed or oddly typed body never fails decoding.
type frappeError struct {
	ExcType        json.RawMessage `json:"exc_type"`
	ServerMessages json.RawMessage `json:"_server_messages"`
	Exception      json.RawMessage `json:"exception"`
}

func rawString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// mapError turns a non-success Frappe response into a domain error. exc_type
// decides first, the HTTP status second. It never returns nil.
func mapError(status int, body []byte) error {
	var fe frappeError
	_ = json.Unmarshal(body, &fe) // best effort; body may be HTML or empty
	excType := rawString(fe.ExcType)

	switch {
	case excType == "DoesNotExistError":
		return fmt.Errorf("%w: %s", erp.ErrNotFound, detail(fe))
	case excType == "PermissionError":
		return fmt.Errorf("%w: %s", erp.ErrForbidden, detail(fe))
	case excType == "AuthenticationError":
		return fmt.Errorf("%w: %s", erp.ErrUnauthenticated, detail(fe))
	case excType == "TimestampMismatchError", excType == "LinkExistsError":
		// LinkExistsError: other documents still depend on this one (cancel/delete)
		return fmt.Errorf("%w: %s", erp.ErrConflict, detail(fe))
	case strings.HasSuffix(excType, "ValidationError") || excType == "MandatoryError" ||
		strings.HasSuffix(excType, "MandatoryError"):
		return validationError(fe)
	case excType != "":
		// An exc_type we do not know: the status still decides below, except
		// that an unknown type on 5xx is the server failing.
	}

	switch {
	case status == 404:
		return fmt.Errorf("%w: %s", erp.ErrNotFound, detail(fe))
	case status == 403:
		return fmt.Errorf("%w: %s", erp.ErrForbidden, detail(fe))
	case status == 401:
		return fmt.Errorf("%w: %s", erp.ErrUnauthenticated, detail(fe))
	case status == 409:
		return fmt.Errorf("%w: %s", erp.ErrConflict, detail(fe))
	case status == 417:
		return validationError(fe)
	case status >= 500:
		return fmt.Errorf("%w: HTTP %d %s", erp.ErrUnavailable, status, excType)
	}
	return fmt.Errorf("erpnext: unexpected response HTTP %d exc_type=%q: %s", status, excType, detail(fe))
}

func validationError(fe frappeError) error {
	msg := detail(fe)
	return &erp.ValidationError{Message: msg}
}

// detail extracts readable text: _server_messages first, then exception.
func detail(fe frappeError) string {
	if msgs := serverMessages(fe.ServerMessages); len(msgs) > 0 {
		return strings.Join(msgs, "; ")
	}
	return cleanText(rawString(fe.Exception))
}

// serverMessages decodes Frappe's doubly encoded message list: a JSON string
// holding a JSON array of JSON-encoded objects with a "message" field.
func serverMessages(raw json.RawMessage) []string {
	encoded := rawString(raw)
	if encoded == "" {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal([]byte(encoded), &items) != nil {
		return nil
	}
	var out []string
	for _, item := range items {
		text := rawString(item) // usually a JSON string containing an object
		if text == "" {
			text = string(item)
		}
		var m struct {
			Message string `json:"message"`
		}
		if json.Unmarshal([]byte(text), &m) != nil {
			continue
		}
		if s := cleanText(m.Message); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func cleanText(s string) string {
	return strings.TrimSpace(html.UnescapeString(htmlTag.ReplaceAllString(s, "")))
}
