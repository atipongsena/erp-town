package platform

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var validKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))

func TestLoadConfigDefaults(t *testing.T) {
	// Every role is on by default; world uses a separate read-only credential.
	c, err := LoadConfig(func(k string) string {
		if k == "TOWN_JWT_PRIVATE_KEY" {
			return validKey
		}
		if k == "TOWN_WORLD_API_KEY" || k == "TOWN_WORLD_API_SECRET" {
			return "world-test"
		}
		return ""
	})
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:8090", c.HTTPAddr)
	assert.Equal(t, "postgres://town:town@127.0.0.1:5433/town?sslmode=disable", c.DatabaseURL)
	assert.Equal(t, "redis://127.0.0.1:6380/0", c.RedisURL)
	assert.Equal(t, "town:events", c.Stream)
	assert.Equal(t, "http://127.0.0.1:8080", c.ERPURL)
	assert.Equal(t, "town.localhost", c.ERPSite)
	assert.Equal(t, validKey, c.JWTPrivateKey)
	assert.True(t, c.Roles.Has(RoleGateway) && c.Roles.Has(RoleOutbox))
	assert.True(t, c.Roles.Has(RoleWorld))
	assert.Equal(t, "world-test", c.WorldAPIKey)
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
	assert.Len(t, r, 3)
}

func TestParseRolesNonBlankWithoutRoles(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
		want    int
	}{
		{"", false, 3},
		{"   ", false, 3},
		{",", true, 0},
		{" , ", true, 0},
		{",,", true, 0},
		{"gateway,", false, 1},
		{", outbox ,", false, 1},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			r, err := ParseRoles(tt.in)
			if tt.wantErr {
				assert.ErrorContains(t, err, "no role")
				return
			}
			require.NoError(t, err)
			assert.Len(t, r, tt.want)
		})
	}
}

func TestParseRolesRejectsUnknown(t *testing.T) {
	_, err := ParseRoles("gateway,unknown")
	assert.ErrorContains(t, err, `unknown role "unknown"`)
	assert.ErrorContains(t, err, "gateway, outbox, world")
}

func TestGatewayRoleRequiresJWTKey(t *testing.T) {
	for _, key := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("too short"))} {
		for _, roles := range []string{"gateway", "world", "", "outbox,gateway"} {
			_, err := LoadConfig(func(k string) string {
				switch k {
				case "TOWN_ROLES":
					return roles
				case "TOWN_JWT_PRIVATE_KEY":
					return key
				}
				return ""
			})
			if assert.ErrorContains(t, err, "TOWN_JWT_PRIVATE_KEY", "roles %q key %q", roles, key) && key != "" {
				assert.NotContains(t, err.Error(), key, "the key value is never echoed")
			}
		}
	}
	// The outbox role alone needs no key.
	_, err := LoadConfig(func(k string) string {
		if k == "TOWN_ROLES" {
			return "outbox"
		}
		return ""
	})
	require.NoError(t, err)
}

func TestWorldRequiresSeparateReadCredentialWithoutEchoingIt(t *testing.T) {
	for _, missing := range []string{"TOWN_WORLD_API_KEY", "TOWN_WORLD_API_SECRET"} {
		env := map[string]string{"TOWN_ROLES": "world", "TOWN_JWT_PRIVATE_KEY": validKey, "TOWN_WORLD_API_KEY": "sensitive-key", "TOWN_WORLD_API_SECRET": "sensitive-secret"}
		env[missing] = ""
		_, err := LoadConfig(func(k string) string { return env[k] })
		require.ErrorContains(t, err, missing)
		assert.NotContains(t, err.Error(), "sensitive")
		env[missing] = "present"
		cfg, err := LoadConfig(func(k string) string { return env[k] })
		require.NoError(t, err)
		assert.True(t, cfg.Roles.Has(RoleWorld))
		assert.False(t, cfg.Roles.Has(RoleGateway))
	}
}
