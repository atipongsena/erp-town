package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"testing/synctest"
	"time"

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
		seed := newSeed(t)
		j, err := NewJWTIssuer(seed, 15*time.Minute)
		require.NoError(t, err)
		tok, _, err := j.Issue(testPrincipal())
		require.NoError(t, err)

		// Tampered payload: flip one byte of the middle segment.
		b := []byte(tok)
		first := 0
		for b[first] != '.' {
			first++
		}
		if b[first+1] == 'A' {
			b[first+1] = 'B'
		} else {
			b[first+1] = 'A'
		}
		_, err = j.Verify(string(b))
		require.Error(t, err)

		// Signed by another key.
		other, err := NewJWTIssuer(newSeed(t), 15*time.Minute)
		require.NoError(t, err)
		otherTok, _, err := other.Issue(testPrincipal())
		require.NoError(t, err)
		_, err = j.Verify(otherTok)
		require.Error(t, err)

		time.Sleep(16 * time.Minute)
		_, err = j.Verify(tok)
		require.Error(t, err)
	})
}

func TestJWTRejectsBadSeed(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 31))
	_, err := NewJWTIssuer(short, time.Minute)
	require.Error(t, err)
	_, err = NewJWTIssuer("not base64!!", time.Minute)
	require.Error(t, err)
}
