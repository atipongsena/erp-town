package erpnext

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentAccessUsesCredentialAndFreshOwnAuthority(t *testing.T) {
	rec := &recorder{reply: func(n int, r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Empty(t, r.Header.Get("X-Town-On-Behalf-Of"))
		assert.Equal(t, "token k:s", r.Header.Get("Authorization"))
		if n == 1 {
			return resp(200, `{"message":{"user":"agent@test","roles":["Sales User"],"companies":["ACME"]}}`), nil
		}
		assert.Equal(t, "/api/method/erp_town.session_api.agent_access", r.URL.Path)
		return resp(200, `{"message":{"user":"agent@test","agent_key":"helper","permissions":{"Sales Order":{"read":true,"create":true}}}}`), nil
	}}
	out, err := NewAdapter(newTestClient(rec)).AgentAccess(t.Context(), keyPrincipal.Credential)
	require.NoError(t, err)
	assert.Equal(t, "helper", out.AgentKey)
	assert.True(t, out.Permissions[erp.DocSalesOrder].Create)
	assert.False(t, out.Permissions[erp.DocPaymentEntry].Create)
	require.Len(t, rec.reqs, 2)
}

func TestDraftAccessUsesOwnFreshCredentialAndNoDelegationHeader(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(fmt.Sprint(allowed), func(t *testing.T) {
			rec := &recorder{reply: func(n int, r *http.Request) (*http.Response, error) {
				if n == 1 {
					return resp(200, `{"message":{"user":"human@test","companies":["ACME"]}}`), nil
				}
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/method/erp_town.session_api.draft_access", r.URL.Path)
				assert.Empty(t, r.Header.Get("X-Town-On-Behalf-Of"))
				assert.Equal(t, "token k:s", r.Header.Get("Authorization"))
				var in erp.DraftAccessInput
				require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
				assert.Equal(t, "CUST", in.Customer)
				assert.Equal(t, []string{"ITEM"}, in.ItemCodes)
				return resp(200, fmt.Sprintf(`{"message":{"allowed":%v}}`, allowed)), nil
			}}
			err := NewAdapter(newTestClient(rec)).CheckDraftAccess(t.Context(), keyPrincipal.Credential, erp.DraftAccessInput{DocType: erp.DocSalesOrder, Company: "ACME", Customer: "CUST", ItemCodes: []string{"ITEM"}})
			if allowed {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, erp.ErrForbidden)
			}
		})
	}
}

func TestAgentAccessRejectsMismatchedAuthorityUser(t *testing.T) {
	rec := &recorder{reply: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return resp(200, `{"message":{"user":"agent@test","companies":["ACME"]}}`), nil
		}
		return resp(200, `{"message":{"user":"other@test","agent_key":"helper"}}`), nil
	}}
	_, err := NewAdapter(newTestClient(rec)).AgentAccess(t.Context(), keyPrincipal.Credential)
	require.ErrorIs(t, err, erp.ErrUnauthenticated)
}
