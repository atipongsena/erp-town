package erpnext

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/atipongsena/erp-town/go/internal/erp/porttest"
)

func TestProjectionAdapterUsesReadOnlyEndpointAndOwnCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/method/erp_town.projection_api.read_projection", r.URL.Path)
		require.Equal(t, "ACME-TH", r.URL.Query().Get("company"))
		require.Equal(t, "token world-key:world-secret", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"message":{"company":"ACME-TH","facts":[{"doctype":"Item","name":"ITEM-1","kind":"stock","scope":"WH-1","fill_ratio":"0.75"}],"sequences":[{"doctype":"Item","name":"ITEM-1","seq":4}]}}`))
	}))
	defer server.Close()
	adapter := NewAdapter(NewClient(ClientConfig{BaseURL: server.URL}))
	as := erp.Principal{Kind: erp.PrincipalSystem, ID: "system.town", Credential: erp.APIKeyCredential{Key: "world-key", Secret: "world-secret"}}
	porttest.RunProjection(t, adapter, as, "ACME-TH")
	out, err := adapter.ReadProjection(t.Context(), as, "ACME-TH")
	require.NoError(t, err)
	require.Len(t, out.Facts, 1)
	require.Equal(t, "0.75", out.Facts[0].FillRatio.String())
	_, err = adapter.ReadProjection(t.Context(), erp.Principal{Kind: erp.PrincipalUser, ID: "sales@example.test", Credential: as.Credential}, "ACME-TH")
	require.ErrorIs(t, err, erp.ErrForbidden)
}

func TestProjectionAdapterRejectsUnsafeOrInconsistentResponse(t *testing.T) {
	for _, body := range []string{
		`{"company":"OTHER","facts":[],"sequences":[]}`,
		`{"company":"ACME-TH","facts":[{"doctype":"Sales Order","name":"SO-1","kind":"document","status":"Submitted","grand_total":123}],"sequences":[]}`,
		`{"company":"ACME-TH","facts":[{"doctype":"Sales Order","name":"SO-1","kind":"document","status":"Submitted"}],"sequences":[]}`,
		`{"company":"ACME-TH","facts":[],"sequences":[{"doctype":"Sales Order","name":"SO-1","seq":-1}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"message":` + body + `}`)) }))
			defer server.Close()
			adapter := NewAdapter(NewClient(ClientConfig{BaseURL: server.URL}))
			_, err := adapter.ReadProjection(t.Context(), erp.Principal{Kind: erp.PrincipalSystem, ID: "system.town", Credential: erp.APIKeyCredential{Key: "test", Secret: "test"}}, "ACME-TH")
			require.Error(t, err)
		})
	}
}
