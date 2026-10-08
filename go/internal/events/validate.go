package events

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var printer = message.NewPrinter(language.English)

const envelopeID = "https://erp-town.local/schema/envelope.json"

// SchemaError is returned when an event does not satisfy the contract.
type SchemaError struct {
	Reasons []string
}

func (e *SchemaError) Error() string {
	return "event does not match schema: " + strings.Join(e.Reasons, "; ")
}

// Validator validates raw event JSON against the embedded envelope schema.
type Validator struct {
	schema *jsonschema.Schema
}

// NewValidator compiles the embedded envelope and per-type schemas. Format
// assertions stay off: the schemas carry patterns, matching the ajv/Python verdicts.
func NewValidator() (*Validator, error) {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	err := fs.WalkDir(schemaFS, "schema", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(p, "schema/fixtures/") || !strings.HasSuffix(p, ".json") {
			return err
		}
		b, err := fs.ReadFile(schemaFS, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		m, _ := doc.(map[string]any)
		id, _ := m["$id"].(string)
		if id == "" {
			return fmt.Errorf("schema %s has no $id", p)
		}
		return c.AddResource(id, doc)
	})
	if err != nil {
		return nil, fmt.Errorf("load schemas: %w", err)
	}
	s, err := c.Compile(envelopeID)
	if err != nil {
		return nil, fmt.Errorf("compile envelope schema: %w", err)
	}
	return &Validator{schema: s}, nil
}

// Validate checks raw against the contract. It returns *SchemaError for content
// that is not valid JSON or does not match the schema.
func (v *Validator) Validate(raw []byte) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return &SchemaError{Reasons: []string{"invalid JSON: " + err.Error()}}
	}
	if err := v.schema.Validate(doc); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			return &SchemaError{Reasons: reasons(ve)}
		}
		return &SchemaError{Reasons: []string{err.Error()}}
	}
	return nil
}

func reasons(ve *jsonschema.ValidationError) []string {
	var out []string
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			loc := "/" + strings.Join(e.InstanceLocation, "/")
			out = append(out, fmt.Sprintf("%s: %s", loc, e.ErrorKind.LocalizedString(printer)))
			return
		}
		for _, c := range e.Causes {
			walk(c)
		}
	}
	walk(ve)
	if len(out) == 0 {
		out = []string{ve.Error()}
	}
	return out
}
