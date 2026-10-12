// Package platform holds process-level concerns: configuration and the HTTP server.
package platform

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Role is a capability a single town process can take on.
type Role string

const (
	RoleGateway Role = "gateway"
	RoleOutbox  Role = "outbox"
	RoleWorld   Role = "world"
)

// KnownRoles lists every valid role.
var KnownRoles = []Role{RoleGateway, RoleOutbox, RoleWorld}

// Roles is a set of enabled roles.
type Roles map[Role]bool

// Has reports whether x is enabled.
func (r Roles) Has(x Role) bool { return r[x] }

// ParseRoles parses a comma-separated role list; blank means all known roles,
// and a non-blank list that names no role (e.g. ",") is an error.
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
	if len(roles) == 0 {
		return nil, fmt.Errorf("no role in %q (leave it blank for all roles)", s)
	}
	return roles, nil
}

// Config is the process configuration, read from TOWN_* environment variables.
type Config struct {
	RequireProvisionedSchema                                                bool
	HTTPAddr, DatabaseURL, RedisURL, Stream, ERPURL, ERPSite, JWTPrivateKey string
	WorldAPIKey, WorldAPISecret                                             string
	Roles                                                                   Roles
	ExperienceEnabled                                                       bool
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
	experience := getenv("TOWN_EXPERIENCE_ENABLED")
	if experience != "" && experience != "true" && experience != "false" {
		return Config{}, errors.New("TOWN_EXPERIENCE_ENABLED must be true or false")
	}
	if experience == "true" && !roles.Has(RoleGateway) {
		return Config{}, errors.New("TOWN_EXPERIENCE_ENABLED requires the gateway role")
	}
	cfg := Config{
		HTTPAddr:          get("TOWN_HTTP_ADDR", "127.0.0.1:8090"),
		DatabaseURL:       get("TOWN_DATABASE_URL", "postgres://town:town@127.0.0.1:5433/town?sslmode=disable"),
		RedisURL:          get("TOWN_REDIS_URL", "redis://127.0.0.1:6380/0"),
		Stream:            get("TOWN_STREAM", "town:events"),
		ERPURL:            get("TOWN_ERP_URL", "http://127.0.0.1:8080"),
		ERPSite:           get("TOWN_ERP_SITE", "town.localhost"),
		JWTPrivateKey:     getenv("TOWN_JWT_PRIVATE_KEY"),
		WorldAPIKey:       getenv("TOWN_WORLD_API_KEY"),
		WorldAPISecret:    getenv("TOWN_WORLD_API_SECRET"),
		Roles:             roles,
		ExperienceEnabled: experience == "true",
	}
	if roles.Has(RoleGateway) || roles.Has(RoleWorld) {
		if err := checkJWTKey(cfg.JWTPrivateKey); err != nil {
			return Config{}, err
		}
	}
	if roles.Has(RoleWorld) {
		if cfg.WorldAPIKey == "" {
			return Config{}, errors.New("TOWN_WORLD_API_KEY is required by the world role (dedicated Town Projection Reader credential)")
		}
		if cfg.WorldAPISecret == "" {
			return Config{}, errors.New("TOWN_WORLD_API_SECRET is required by the world role (dedicated Town Projection Reader credential)")
		}
	}
	return cfg, nil
}

// checkJWTKey requires the standard base64 of a 32-byte Ed25519 seed. Errors
// never include the key itself.
func checkJWTKey(key string) error {
	if key == "" {
		return errors.New("TOWN_JWT_PRIVATE_KEY is required by the gateway/world roles " +
			"(standard base64 of a 32-byte Ed25519 seed)")
	}
	seed, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return errors.New("TOWN_JWT_PRIVATE_KEY is not valid standard base64")
	}
	if len(seed) != ed25519.SeedSize {
		return fmt.Errorf("TOWN_JWT_PRIVATE_KEY must decode to %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	return nil
}
