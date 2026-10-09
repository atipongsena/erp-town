package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

func newSeed(t *testing.T) string {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(seed)
}

func testPrincipal() erp.Principal {
	return erp.Principal{
		Kind: erp.PrincipalUser, ID: "alice@example.com",
		Roles: []string{"Sales User", "Accounts User"}, Companies: []string{"TOWN", "TWO"},
	}
}

func TestJWTRoundTrip(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		j, err := NewJWTIssuer(newSeed(t), 15*time.Minute)
		require.NoError(t, err)
		tok, exp, err := j.Issue(testPrincipal())
		require.NoError(t, err)
		assert.Equal(t, time.Now().Add(15*time.Minute), exp)

		c, err := j.Verify(tok)
		require.NoError(t, err)
		assert.Equal(t, "alice@example.com", c.Subject)
		assert.Equal(t, []string{"Sales User", "Accounts User"}, c.Roles)
		assert.Equal(t, []string{"TOWN", "TWO"}, c.Companies)
		assert.True(t, c.ExpiresAt.Equal(time.Now().Add(15*time.Minute)), "exp %v", c.ExpiresAt)
	})
}

func TestJWTRejectsExpiredAndTampered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		j, err := NewJWTIssuer(newSeed(t), 15*time.Minute)
		require.NoError(t, err)
		tok, _, err := j.Issue(testPrincipal())
		require.NoError(t, err)

		// Tampered payload: valid JSON with different claims, original signature.
		parts := strings.Split(tok, ".")
		require.Len(t, parts, 3)
		raw, err := base64.RawURLEncoding.DecodeString(parts[1])
		require.NoError(t, err)
		var m map[string]any
		require.NoError(t, json.Unmarshal(raw, &m))
		m["roles"] = []string{"System Manager"}
		forged, err := json.Marshal(m)
		require.NoError(t, err)
		parts[1] = base64.RawURLEncoding.EncodeToString(forged)
		_, err = j.Verify(strings.Join(parts, "."))
		require.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)
		require.ErrorIs(t, err, erp.ErrUnauthenticated)

		// Signed by another key.
		other, err := NewJWTIssuer(newSeed(t), 15*time.Minute)
		require.NoError(t, err)
		otherTok, _, err := other.Issue(testPrincipal())
		require.NoError(t, err)
		_, err = j.Verify(otherTok)
		require.ErrorIs(t, err, jwt.ErrTokenSignatureInvalid)
		require.ErrorIs(t, err, erp.ErrUnauthenticated)

		time.Sleep(16 * time.Minute)
		_, err = j.Verify(tok)
		require.ErrorIs(t, err, jwt.ErrTokenExpired)
		require.ErrorIs(t, err, erp.ErrUnauthenticated)
	})
}

func TestJWTRejectsOtherAlgorithms(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		j, err := NewJWTIssuer(newSeed(t), 15*time.Minute)
		require.NoError(t, err)
		claims := tokenClaims{
			Roles: []string{"System Manager"}, Companies: []string{"TOWN"},
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "mallory",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
		}
		hs, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("secret"))
		require.NoError(t, err)
		none, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
		require.NoError(t, err)
		for name, tok := range map[string]string{"HS256": hs, "none": none} {
			_, err := j.Verify(tok)
			require.ErrorIs(t, err, erp.ErrUnauthenticated, name)
		}
	})
}

func TestJWTRejectsBadSeed(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 31))
	_, err := NewJWTIssuer(short, time.Minute)
	require.Error(t, err)
	_, err = NewJWTIssuer("not base64!!", time.Minute)
	require.Error(t, err)
}
