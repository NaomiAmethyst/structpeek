// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

// Package schematest checks, for tests, that structpeek's schemas are
// valid JSON Schema and fit the data they came from.
package schematest

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/NaomiAmethyst/structpeek/internal/decode"
)

// Validate fails t unless schema compiles as JSON Schema 2020-12, formats
// and all, and each of docs that JSON can hold is valid against it.
func Validate(t *testing.T, schema []byte, docs ...any) {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		t.Fatalf("schema isn't JSON: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.AssertFormat()
	if err := c.AddResource("schema.json", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("schema.json")
	if err != nil {
		t.Fatalf("schema doesn't compile: %v\n%s", err, schema)
	}
	for _, d := range docs {
		b, err := json.Marshal(decode.Plain(d))
		if err != nil {
			// NaN or Infinity, as YAML, TOML and JSON5 have: JSON can't
			// hold them, so JSON Schema can't validate them.
			t.Logf("can't check data against its schema: %v", err)
			continue
		}
		inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(inst); err != nil {
			t.Errorf("data doesn't fit its schema: %v\n%s", err, schema)
		}
	}
}
