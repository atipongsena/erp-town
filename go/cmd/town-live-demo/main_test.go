package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/atipongsena/erp-town/go/internal/auth"
	"github.com/atipongsena/erp-town/go/internal/erp/erpfake"
	"github.com/atipongsena/erp-town/go/internal/platform"
	"github.com/atipongsena/erp-town/go/internal/world"
)

func TestLoopbackBoundary(t *testing.T) {
	for _, tc := range []struct {
		host, origin, fetchSite string
		allowed                 bool
	}{
		{"127.0.0.1:5196", "", "", true},
		{"localhost:5196", "http://localhost:5196", "same-origin", true},
		{"live-demo.localhost:5196", "http://live-demo.localhost:5196", "same-origin", true},
		{"example.com:5196", "", "", false},
		{"127.0.0.1:5196", "https://evil.example", "cross-site", false},
		{"127.0.0.1:5196", "http://127.0.0.1:5197", "same-site", false},
	} {
		r := httptest.NewRequest("GET", "http://"+tc.host+"/", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.fetchSite)
		if got := allowedRequest(r, "5196"); got != tc.allowed {
			t.Fatalf("boundary %q: got %v", tc.host, got)
		}
	}
}

func TestAllAPIRoutesRegisterOnce(t *testing.T) {
	issuer, err := auth.NewJWTIssuer(base64.StdEncoding.EncodeToString(make([]byte, 32)), 20*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mux := platform.NewMux()
	adapter := erpfake.New(erpfake.Config{})
	// Registration uses the exact production host function. No database or ERP
	// calls are needed to catch overlapping ServeMux route ownership.
	registerAPI(mux, nil, adapter, issuer, world.NewService(t.Context()), nil, world.NewRedisHistory(nil, stream), nil)
	for _, path := range []string{"/town/api/me", "/town/api/live-demo", "/town/api/agent-runs", "/town/api/history", "/town/api/avatar/preference", "/town/api/avatar/presence", "/town/ws"} {
		r := httptest.NewRequest(http.MethodGet, "http://live-demo.localhost:5196"+path, nil)
		_, pattern := mux.Handler(r)
		if pattern != "GET "+path {
			t.Fatalf("%s has wrong route owner %q", path, pattern)
		}
	}
}
