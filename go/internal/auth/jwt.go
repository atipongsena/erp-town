package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/atipongsena/erp-town/go/internal/erp"
)

// Claims are the verified contents of a town JWT.
type Claims struct {
	Subject   string
	Roles     []string
	Companies []string
	ExpiresAt time.Time
}

type tokenClaims struct {
	Roles     []string `json:"roles"`
	Companies []string `json:"companies"`
	jwt.RegisteredClaims
}

// JWTIssuer signs and verifies EdDSA (Ed25519) tokens.
type JWTIssuer struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	ttl  time.Duration
}

// NewJWTIssuer builds an issuer from the standard base64 of a 32-byte
// Ed25519 seed.
func NewJWTIssuer(seedBase64 string, ttl time.Duration) (*JWTIssuer, error) {
	seed, err := base64.StdEncoding.DecodeString(seedBase64)
	if err != nil {
		return nil, fmt.Errorf("auth: jwt seed is not valid base64: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("auth: jwt seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return &JWTIssuer{priv: priv, pub: priv.Public().(ed25519.PublicKey), ttl: ttl}, nil
}

// Issue signs a token for p. expires is truncated to whole seconds, the JWT
// exp resolution.
func (j *JWTIssuer) Issue(p erp.Principal) (string, time.Time, error) {
	now := time.Now().Truncate(time.Second)
	expires := now.Add(j.ttl)
	claims := tokenClaims{
		Roles:     p.Roles,
		Companies: p.Companies,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   p.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(j.priv)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign jwt: %w", err)
	}
	return tok, expires, nil
}

// Verify checks signature (EdDSA only) and expiry and returns the claims.
func (j *JWTIssuer) Verify(token string) (Claims, error) {
	var tc tokenClaims
	_, err := jwt.ParseWithClaims(token, &tc,
		func(*jwt.Token) (any, error) { return j.pub, nil },
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(time.Now),
	)
	if err != nil {
		return Claims{}, fmt.Errorf("auth: verify jwt: %w", err)
	}
	var exp time.Time
	if tc.ExpiresAt != nil {
		exp = tc.ExpiresAt.Time
	}
	return Claims{Subject: tc.Subject, Roles: tc.Roles, Companies: tc.Companies, ExpiresAt: exp}, nil
}
