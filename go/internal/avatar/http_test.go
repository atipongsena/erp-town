package avatar

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atipongsena/erp-town/go/internal/erp"
	"github.com/stretchr/testify/require"
)

func TestAvatarHTTPRejectsCrossUserUnknownDuplicateAndMissingCSRF(t *testing.T) {
	s, _, p, _ := fixture(t)
	h := NewHandler(s, func(*http.Request) (erp.Principal, error) { return p, nil })
	for _, body := range []string{`{"company":"ACME","consent":true,"appearance":"human-blue","user":"other"}`, `{"company":"ACME","company":"OTHER","consent":true,"appearance":"human-blue"}`, `{"company":"ACME","consent":null,"appearance":"human-blue"}`} {
		r := httptest.NewRequest(http.MethodPut, "/town/api/avatar/preference", strings.NewReader(body))
		r.Header.Set("X-Town-Request", "1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
	r := httptest.NewRequest(http.MethodPut, "/town/api/avatar/preference", strings.NewReader(`{"company":"ACME","consent":true,"appearance":"human-blue"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
	r.Header.Set("X-Town-Request", "1")
	r.Body = http.NoBody
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
