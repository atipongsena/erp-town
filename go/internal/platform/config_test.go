package platform

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigDefaults(t *testing.T) {
	c, err := LoadConfig(func(string) string { return "" })
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:8090", c.HTTPAddr)
	assert.Equal(t, "postgres://town:town@127.0.0.1:5433/town?sslmode=disable", c.DatabaseURL)
	assert.Equal(t, "redis://127.0.0.1:6380/0", c.RedisURL)
	assert.Equal(t, "town:events", c.Stream)
	assert.Equal(t, "http://127.0.0.1:8080", c.ERPURL)
	assert.Equal(t, "town.localhost", c.ERPSite)
	assert.Empty(t, c.JWTPrivateKey)
	assert.True(t, c.Roles.Has(RoleGateway) && c.Roles.Has(RoleOutbox))
}

func TestLoadConfigOverrides(t *testing.T) {
	env := map[string]string{
		"TOWN_HTTP_ADDR":       "0.0.0.0:9000",
		"TOWN_DATABASE_URL":    "postgres://x",
		"TOWN_REDIS_URL":       "redis://y",
		"TOWN_STREAM":          "s",
		"TOWN_ERP_URL":         "http://erp",
		"TOWN_ERP_SITE":        "site.test",
		"TOWN_JWT_PRIVATE_KEY": "key",
		"TOWN_ROLES":           "outbox",
	}
	c, err := LoadConfig(func(k string) string { return env[k] })
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0:9000", c.HTTPAddr)
	assert.Equal(t, "postgres://x", c.DatabaseURL)
	assert.Equal(t, "redis://y", c.RedisURL)
	assert.Equal(t, "s", c.Stream)
	assert.Equal(t, "http://erp", c.ERPURL)
	assert.Equal(t, "site.test", c.ERPSite)
	assert.Equal(t, "key", c.JWTPrivateKey)
	assert.True(t, c.Roles.Has(RoleOutbox))
	assert.False(t, c.Roles.Has(RoleGateway))
}

func TestLoadConfigRejectsBadRoles(t *testing.T) {
	_, err := LoadConfig(func(k string) string {
		if k == "TOWN_ROLES" {
			return "nope"
		}
		return ""
	})
	assert.ErrorContains(t, err, `unknown role "nope"`)
}

func TestParseRoles(t *testing.T) {
	r, err := ParseRoles(" gateway , outbox ")
	require.NoError(t, err)
	assert.True(t, r.Has(RoleGateway) && r.Has(RoleOutbox))
	r, err = ParseRoles("")
	require.NoError(t, err)
	assert.Len(t, r, 2)
}

func TestParseRolesRejectsUnknown(t *testing.T) {
	_, err := ParseRoles("gateway,world")
	assert.ErrorContains(t, err, `unknown role "world"`)
	assert.ErrorContains(t, err, "gateway, outbox")
}
