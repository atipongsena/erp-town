package avatar

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

type (
	Resolve func(*http.Request) (erp.Principal, error)
	Handler struct {
		service *Service
		resolve Resolve
	}
)

func NewHandler(s *Service, r Resolve) *Handler { return &Handler{service: s, resolve: r} }

// Register is explicit; services are disabled until a host supplies a database
// and fresh session resolver. It does not change gateway identity or ERP writes.
func (h *Handler) Register(mux *http.ServeMux) {
	for _, route := range []string{"GET /town/api/avatar/preference", "PUT /town/api/avatar/preference", "GET /town/api/avatar/presence", "POST /town/api/avatar/presence"} {
		mux.Handle(route, h)
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if h.resolve == nil || h.service == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Header.Get("X-Town-Request") != "1" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	p, err := h.resolve(r)
	if err != nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	var out any
	switch r.Method + " " + r.URL.Path {
	case "GET /town/api/avatar/preference":
		out, err = h.service.Preference(ctx, p, r.URL.Query().Get("company"))
	case "GET /town/api/avatar/presence":
		out, err = h.service.List(ctx, p, r.URL.Query().Get("company"))
	case "PUT /town/api/avatar/preference":
		var in struct {
			Company    string `json:"company"`
			Consent    bool   `json:"consent"`
			Appearance string `json:"appearance"`
		}
		if decodeFlat(w, r, &in) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		err = h.service.SetPreference(ctx, p, in.Company, Preference{Consent: in.Consent, Appearance: in.Appearance})
		out = map[string]bool{"ok": true}
	case "POST /town/api/avatar/presence":
		var in struct {
			Company    string `json:"company"`
			Department string `json:"department"`
			Emote      string `json:"emote"`
		}
		if decodeFlat(w, r, &in) != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		out, err = h.service.Update(ctx, p, in.Company, in.Department, in.Emote)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		status := 403
		if err == erp.ErrValidation {
			status = 400
		}
		if err == erp.ErrUnavailable {
			status = 503
		}
		http.Error(w, "request denied", status)
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

func decodeFlat(w http.ResponseWriter, r *http.Request, out any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return erp.ErrValidation
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := t.(string)
		if !ok {
			return erp.ErrValidation
		}
		if _, exists := fields[key]; exists {
			return erp.ErrValidation
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return err
		}
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || raw[0] == '{' || raw[0] == '[' {
			return erp.ErrValidation
		}
		fields[key] = raw
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return erp.ErrValidation
	}
	b, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	typed := json.NewDecoder(bytes.NewReader(b))
	typed.DisallowUnknownFields()
	return typed.Decode(out)
}
