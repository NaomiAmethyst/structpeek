// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package infer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/NaomiAmethyst/structpeek/internal/decode"
	"github.com/NaomiAmethyst/structpeek/internal/schematest"
)

// runWith is run, with options.
func runWith(t *testing.T, o Options, src string) string {
	t.Helper()
	doc, err := decode.Decode([]byte(src), decode.JSON, decode.Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Marshal(Infer([]any{doc}, o), "")
	if err != nil {
		t.Fatal(err)
	}
	schematest.Validate(t, b, doc)
	return string(b)
}

func noneOf(t *testing.T, out string, values ...string) {
	t.Helper()
	for _, v := range values {
		if strings.Contains(out, `"`+v+`"`) {
			t.Errorf("%q in schema:\n%s", v, out)
		}
	}
}

// Kinds of record told apart by a field are tagged with its values: with
// enums off, they mustn't be, and they mustn't be anyway unless they're
// as repeated as an enum's would have to be.
func TestTagsFollowEnumRules(t *testing.T) {
	shapes := func(n int) string {
		return records(n, func(i int) string {
			if i%2 == 0 {
				return fmt.Sprintf(`{"name": "Alice", "age": %d, "city": "x"}`, i)
			}
			return fmt.Sprintf(`{"name": "Bob", "plan": "p", "seats": %d, "owner": true}`, i)
		})
	}
	noEnums := DefaultOptions()
	noEnums.MaxEnum = 0
	noneOf(t, runWith(t, noEnums, shapes(200)), "Alice", "Bob")
	noneOf(t, runWith(t, DefaultOptions(), shapes(10)), "Alice", "Bob")
	if out := runWith(t, DefaultOptions(), shapes(200)); !strings.Contains(out, `"const":"Alice"`) {
		t.Errorf("no tag when enums would be shown:\n%s", out)
	}
}

// A map's values are pooled; if any of its keys sounds secret, they all
// count as secret.
func TestMapsKeepSensitiveKeys(t *testing.T) {
	var parts []string
	for i := range 21 {
		parts = append(parts, fmt.Sprintf(`"%d": "hunter2"`, i))
	}
	parts = append(parts, `"password": "hunter2"`)
	out := runWith(t, DefaultOptions(), `{"m": {`+strings.Join(parts, ", ")+`}}`)
	noneOf(t, out, "hunter2")

	nums := strings.ReplaceAll(strings.Join(parts, ", "), `"hunter2"`, "1234")
	out = runWith(t, DefaultOptions(), `{"m": {`+nums+`}}`)
	if strings.Contains(out, "maximum") {
		t.Errorf("range for a map with a secret key:\n%s", out)
	}
}

// Name/value lists, like environment variables, say what's secret in a
// value rather than a key.
func TestNamedValues(t *testing.T) {
	out := runWith(t, DefaultOptions(), records(60, func(i int) string {
		name := []string{"LOG_LEVEL", "DB_PASSWORD", "REGION"}[i%3]
		return fmt.Sprintf(`{"name": %q, "value": "hunter2"}`, name)
	}))
	noneOf(t, out, "hunter2")

	// XML-style too: <param name="api_key" value="..."/>.
	out = runWith(t, DefaultOptions(), records(60, func(i int) string {
		return fmt.Sprintf(`{"param": {"name": %q, "value": "s3cret"}}`, []string{"api_key", "colour"}[i%2])
	}))
	noneOf(t, out, "s3cret")

	// A secret-sounding value elsewhere isn't a name, and doesn't hide
	// anything.
	out = runWith(t, DefaultOptions(), records(60, func(i int) string {
		return fmt.Sprintf(`{"kind": "Secret", "method": "card", "status": %q}`, []string{"open", "shut"}[i%2])
	}))
	if !strings.Contains(out, `"enum":["open","shut"]`) {
		t.Errorf("status enum lost:\n%s", out)
	}
}
