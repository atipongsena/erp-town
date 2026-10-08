package events

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Envelope is the town event envelope (CloudEvents 1.0 based). Time is kept as the
// original ISO 8601 string. Data is the per-type payload, validated by the schema.
type Envelope struct {
	SpecVersion   string          `json:"specversion"`
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Source        string          `json:"source"`
	Time          string          `json:"time"`
	SchemaVersion int             `json:"schema_version"`
	Company       *string         `json:"company"`
	Actor         Actor           `json:"actor"`
	Subject       Subject         `json:"subject"`
	Seq           int64           `json:"seq"`
	CorrelationID string          `json:"correlation_id"`
	CausationID   *string         `json:"causation_id"`
	Visibility    Visibility      `json:"visibility"`
	Data          json.RawMessage `json:"data"`
}

// Actor is who caused the event.
type Actor struct {
	Kind       string  `json:"kind"`
	ID         string  `json:"id"`
	OnBehalfOf *string `json:"on_behalf_of"`
}

// Subject is the document the event is about.
type Subject struct {
	DocType string `json:"doctype"`
	Name    string `json:"name"`
}

// Visibility says who may see the event. Roles always encodes as an array.
type Visibility struct {
	Level string   `json:"level"`
	Roles []string `json:"roles"`
}

// MarshalJSON encodes a nil Roles slice as [] rather than null.
func (v Visibility) MarshalJSON() ([]byte, error) {
	type plain Visibility
	if v.Roles == nil {
		v.Roles = []string{}
	}
	return marshalNoEscape(plain(v))
}

// Marshal encodes the envelope as compact JSON without HTML escaping and without
// a trailing newline.
func Marshal(e Envelope) ([]byte, error) {
	if len(e.Data) == 0 {
		e.Data = json.RawMessage("null")
	}
	return marshalNoEscape(e)
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Decode validates raw against the contract and decodes it into an Envelope.
// Schema failures are returned as *SchemaError.
func Decode(v *Validator, raw []byte) (Envelope, error) {
	if err := v.Validate(raw); err != nil {
		return Envelope{}, err
	}
	var e Envelope
	if err := json.Unmarshal(raw, &e); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	return e, nil
}

// DecodeData decodes the envelope's data payload into T, rejecting unknown fields.
func DecodeData[T any](e Envelope) (T, error) {
	var out T
	dec := json.NewDecoder(bytes.NewReader(e.Data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("decode %s data: %w", e.Type, err)
	}
	return out, nil
}
