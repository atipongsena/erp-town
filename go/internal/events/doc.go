// Package events implements the ERP Town event contract in Go: envelope types,
// JSON Schema validation and (de)serialization. The schemas and the per-type
// data structs are generated from packages/event-schema by `pnpm schema:gen`.
package events

import "embed"

// schemaFS holds the generated copy of packages/event-schema (schema + fixtures).
//
//go:embed schema
var schemaFS embed.FS
