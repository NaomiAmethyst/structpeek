// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package infer

import (
	"bytes"
	"encoding/json"
	"reflect"
)

// Draft is the JSON Schema dialect structpeek writes.
const Draft = "https://json-schema.org/draft/2020-12/schema"

// Schema is the subset of JSON Schema 2020-12 that structpeek writes.
// Fields are in the order they're written out.
type Schema struct {
	Schema               string      `json:"$schema,omitempty"`
	Ref                  string      `json:"$ref,omitempty"`
	Description          string      `json:"description,omitempty"`
	Type                 any         `json:"type,omitempty"` // string or []string
	Format               string      `json:"format,omitempty"`
	Pattern              string      `json:"pattern,omitempty"`
	Const                *string     `json:"const,omitempty"`
	Enum                 []string    `json:"enum,omitempty"`
	Minimum              json.Number `json:"minimum,omitempty"`
	Maximum              json.Number `json:"maximum,omitempty"`
	Properties           *Properties `json:"properties,omitempty"`
	Required             []string    `json:"required,omitempty"`
	MinProperties        *int        `json:"minProperties,omitempty"`
	MaxProperties        *int        `json:"maxProperties,omitempty"`
	PropertyNames        *Schema     `json:"propertyNames,omitempty"`
	AdditionalProperties any         `json:"additionalProperties,omitempty"` // *Schema or false
	PrefixItems          []*Schema   `json:"prefixItems,omitempty"`
	Items                any         `json:"items,omitempty"` // *Schema or false
	MinItems             *int        `json:"minItems,omitempty"`
	MaxItems             *int        `json:"maxItems,omitempty"`
	AnyOf                []*Schema   `json:"anyOf,omitempty"`
	Defs                 *Properties `json:"$defs,omitempty"`
}

// typeOnly reports whether s says nothing but its type, so it can be
// folded into a list of types.
func (s *Schema) typeOnly() bool {
	if _, ok := s.Type.(string); !ok {
		return false
	}
	rest := *s
	rest.Type = nil
	return reflect.ValueOf(rest).IsZero()
}

// Properties is a map of names to schemas that keeps its order.
type Properties struct {
	keys []string
	m    map[string]*Schema
}

// Set adds or replaces a schema.
func (p *Properties) Set(k string, s *Schema) {
	if p.m == nil {
		p.m = map[string]*Schema{}
	}
	if _, ok := p.m[k]; !ok {
		p.keys = append(p.keys, k)
	}
	p.m[k] = s
}

// Get returns the schema for k, or nil.
func (p *Properties) Get(k string) *Schema {
	return p.m[k]
}

// Keys returns the names in order.
func (p *Properties) Keys() []string {
	return p.keys
}

// MarshalJSON writes the properties in order.
func (p *Properties) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	buf.WriteByte('{')
	for i, k := range p.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := enc.Encode(k); err != nil {
			return nil, err
		}
		buf.WriteByte(':')
		if err := enc.Encode(p.m[k]); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Marshal writes a schema as JSON, indented unless indent is empty.
func Marshal(s *Schema, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
