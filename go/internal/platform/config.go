// Package platform holds process-level concerns: configuration and the HTTP server.
package platform

import (
	"fmt"
	"strings"
)

// Role is a capability a single town process can take on.
type Role string

const (
	RoleGateway Role = "gateway"
	RoleOutbox  Role = "outbox"
)

// KnownRoles lists every valid role.
var KnownRoles = []Role{RoleGateway, RoleOutbox}

// Roles is a set of enabled roles.
type Roles map[Role]bool

// Has reports whether x is enabled.
func (r Roles) Has(x Role) bool { return r[x] }

// ParseRoles parses a comma-separated role list; empty means all known roles.
func ParseRoles(s string) (Roles, error) {
	roles := Roles{}
	if strings.TrimSpace(s) == "" {
		for _, k := range KnownRoles {
			roles[k] = true
		}
		return roles, nil
	}
	for _, part := range strings.Split(s, ",") {
		name := Role(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		known := false
		for _, k := range KnownRoles {
			if k == name {
				known = true
			}
		}
		if !known {
			names := make([]string, len(KnownRoles))
			for i, k := range KnownRoles {
				names[i] = string(k)
			}
			return nil, fmt.Errorf("unknown role %q (known roles: %s)", name, strings.Join(names, ", "))
		}
		roles[name] = true
	}
	return roles, nil
}

// Config is the process configuration, read from TOWN_* environment variables.
type Config struct {
	HTTPAddr, DatabaseURL, RedisURL, Stream, ERPURL, ERPSite, JWTPrivateKey string
	Roles                                                                   Roles
}

// LoadConfig reads configuration through getenv, applying dev defaults.
func LoadConfig(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	roles, err := ParseRoles(getenv("TOWN_ROLES"))
	if err != nil {
		return Config{}, fmt.Errorf("TOWN_ROLES: %w", err)
	}
	return Config{
		HTTPAddr:      get("TOWN_HTTP_ADDR", "127.0.0.1:8090"),
		DatabaseURL:   get("TOWN_DATABASE_URL", "postgres://town:town@127.0.0.1:5433/town?sslmode=disable"),
		RedisURL:      get("TOWN_REDIS_URL", "redis://127.0.0.1:6380/0"),
		Stream:        get("TOWN_STREAM", "town:events"),
		ERPURL:        get("TOWN_ERP_URL", "http://127.0.0.1:8080"),
		ERPSite:       get("TOWN_ERP_SITE", "town.localhost"),
		JWTPrivateKey: getenv("TOWN_JWT_PRIVATE_KEY"),
		Roles:         roles,
	}, nil
}
