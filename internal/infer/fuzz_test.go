// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package infer

import (
	"testing"

	"github.com/NaomiAmethyst/structpeek/internal/decode"
	"github.com/NaomiAmethyst/structpeek/internal/schematest"
)

// FuzzInfer checks that for any JSON, the schema is valid and the JSON
// fits it.
func FuzzInfer(f *testing.F) {
	for _, seed := range []string{
		`{"a": [1, "x", null, {"b": true}]}`, `[[1, "a"], [2, "b"]]`, `[{"t": "x", "a": 1}, {"t": "y", "b": 2}]`,
		`{"1": {"u": 1, "v": 2}, "2": {"u": 3}}`, `[{"add": 1}, {"remove": "x"}, {"add": 2}]`,
		`[1.5, -3, 1e300, 0]`, `["2024-01-01", "2024-02-02"]`, `[[], {}, "", null]`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := decode.Decode(data, decode.JSON, decode.Options{})
		if err != nil {
			return
		}
		b, err := Marshal(Infer([]any{doc}, withValues()), "")
		if err != nil {
			t.Fatal(err)
		}
		schematest.Validate(t, b, doc)
	})
}
