package porttest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// RunProjection checks the read capability separately from the ERP write port.
// The caller supplies a stable backend fixture and its own reader credential.
func RunProjection(t *testing.T, reader erp.Projection, as erp.Principal, company string) {
	t.Helper()
	t.Run("CompanyMetadataIsCompleteAndReadOnly", func(t *testing.T) {
		first, err := reader.ReadProjection(t.Context(), as, company)
		require.NoError(t, err)
		require.NoError(t, first.Validate(company))
		second, err := reader.ReadProjection(t.Context(), as, company)
		require.NoError(t, err)
		require.Equal(t, first, second, "a read cannot advance sequences or change business state")
	})
	t.Run("CallerCannotExpandCapability", func(t *testing.T) {
		for _, change := range []func(*erp.Principal){
			func(p *erp.Principal) { p.Credential = nil },
			func(p *erp.Principal) { p.Kind = erp.PrincipalUser },
			func(p *erp.Principal) { p.ID = "agent.1" },
			func(p *erp.Principal) { p.OnBehalfOf = "user.other" },
		} {
			bad := as
			change(&bad)
			_, err := reader.ReadProjection(t.Context(), bad, company)
			if bad.Credential == nil {
				require.ErrorIs(t, err, erp.ErrUnauthenticated)
			} else {
				require.ErrorIs(t, err, erp.ErrForbidden)
			}
		}
		_, err := reader.ReadProjection(t.Context(), as, " ")
		require.Error(t, err)
	})
}
