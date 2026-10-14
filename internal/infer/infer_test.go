// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package infer

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/NaomiAmethyst/structpeek/internal/decode"
	"github.com/NaomiAmethyst/structpeek/internal/schematest"
)

// run infers a schema for data given as JSON, checks it's valid and fits,
// and returns it.
func run(t *testing.T, src string) *Schema {
	t.Helper()
	doc, err := decode.Decode([]byte(src), decode.JSON, decode.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return runDocs(t, doc)
}

func runDocs(t *testing.T, docs ...any) *Schema {
	t.Helper()
	s := Infer(docs, DefaultOptions())
	b, err := Marshal(s, "  ")
	if err != nil {
		t.Fatal(err)
	}
	schematest.Validate(t, b, docs...)
	return s
}

// at follows a path of property names, with "[]" for array items and
// numbers for anyOf branches or tuple positions.
func at(t *testing.T, s *Schema, path ...string) *Schema {
	t.Helper()
	for _, p := range path {
		var next *Schema
		switch {
		case p == "[]":
			next, _ = s.Items.(*Schema)
		case p == "*":
			next, _ = s.AdditionalProperties.(*Schema)
		case strings.HasPrefix(p, "#"):
			var i int
			fmt.Sscan(p[1:], &i)
			if i < len(s.AnyOf) {
				next = s.AnyOf[i]
			} else if i < len(s.PrefixItems) {
				next = s.PrefixItems[i]
			}
		case s.Properties != nil:
			next = s.Properties.Get(p)
		}
		if next == nil {
			b, _ := Marshal(s, "  ")
			t.Fatalf("no %q in %v of\n%s", p, path, b)
		}
		s = next
	}
	return s
}

func js(s *Schema) string {
	b, _ := Marshal(s, "")
	return strings.TrimSpace(string(b))
}

func want(t *testing.T, s *Schema, expected string) {
	t.Helper()
	if got := js(s); got != expected {
		t.Errorf("got  %s\nwant %s", got, expected)
	}
}

// records makes a JSON array of n objects from a function of i.
func records(n int, f func(i int) string) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = f(i)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// Seeded, so a token that happens to look like words can't make a test
// flaky.
var rng = rand.NewChaCha8([32]byte{'s', 't', 'r', 'u', 'c', 't', 'p', 'e', 'e', 'k'})

func randHex(n int) string {
	b := make([]byte, n)
	rng.Read(b)
	return hex.EncodeToString(b)
}

func randB64(n int) string {
	b := make([]byte, n)
	rng.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// TestHighEntropyRate checks random tokens are almost always spotted.
func TestHighEntropyRate(t *testing.T) {
	missed := 0
	for range 10000 {
		if !highEntropy(randB64(24)) || !highEntropy(randHex(16)) {
			missed++
		}
	}
	if missed > 25 {
		t.Errorf("missed %d of 10000 pairs of random tokens", missed)
	}
}

func TestObjects(t *testing.T) {
	s := run(t, records(10, func(i int) string {
		if i%2 == 0 {
			return fmt.Sprintf(`{"a": %d, "b": "x", "c": true}`, i)
		}
		return fmt.Sprintf(`{"a": "%d", "b": "y"}`, i)
	}))
	item := at(t, s, "[]")
	if !reflect.DeepEqual(item.Required, []string{"a", "b"}) {
		t.Errorf("required = %v", item.Required)
	}
	if item.AdditionalProperties != false {
		t.Errorf("not closed: %v", item.AdditionalProperties)
	}
	want(t, at(t, item, "a"), `{"type":["integer","string"],"minimum":0,"maximum":10}`)
}

func TestKinds(t *testing.T) {
	s := run(t, records(60, func(i int) string {
		if i%2 == 0 {
			return fmt.Sprintf(`{"kind": "circle", "r": %d}`, i)
		}
		return fmt.Sprintf(`{"kind": "rect", "w": %d, "h": 1, "label": "x"}`, i)
	}))
	items := at(t, s, "[]")
	if len(items.AnyOf) != 2 {
		t.Fatalf("want 2 kinds, got %s", js(items))
	}
	want(t, at(t, items, "#0", "kind"), `{"type":"string","const":"circle"}`)
	want(t, at(t, items, "#1", "kind"), `{"type":"string","const":"rect"}`)

	// Alike enough to merge: the odd key out just becomes optional.
	s = run(t, records(20, func(i int) string {
		if i%2 == 0 {
			return `{"a": 1, "b": 2, "c": 3}`
		}
		return `{"a": 1, "b": 2, "d": 3}`
	}))
	if got := at(t, s, "[]").Required; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("required = %v", got)
	}
}

func TestOneOfKeys(t *testing.T) {
	s := run(t, `[{"add": {"path": "a"}}, {"remove": "b"}, {"add": {"path": "c"}}]`)
	want(t, at(t, s, "[]"), `{"type":"object","properties":{"add":{"type":"object","properties":`+
		`{"path":{"type":"string"}},"required":["path"],"additionalProperties":false},`+
		`"remove":{"type":"string"}},"minProperties":1,"maxProperties":1,"additionalProperties":false}`)

	// Always the same key is just an object.
	s = run(t, `[{"a": 1}, {"a": 2}]`)
	if got := at(t, s, "[]").Required; !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("required = %v", got)
	}
}

func TestStrings(t *testing.T) {
	s := run(t, records(100, func(i int) string {
		return fmt.Sprintf(`{"status": %q, "password": %q, "token": %q, "name": "user %d", `+
			`"when": "2024-01-%02dT10:00:00Z", "site": "https://example.com/%d", "ip": "10.0.0.%d"}`,
			[]string{"open", "closed"}[i%2], []string{"hunter2", "letmein"}[i%2], randHex(20), i,
			i%28+1, i, i)
	}))
	item := at(t, s, "[]")
	want(t, at(t, item, "status"), `{"type":"string","enum":["closed","open"]}`)
	want(t, at(t, item, "password"), `{"$ref":"#/$defs/lowCardinalityString"}`)
	want(t, at(t, item, "token"), `{"$ref":"#/$defs/highEntropyString"}`)
	want(t, at(t, item, "name"), `{"type":"string"}`)
	want(t, at(t, item, "when"), `{"type":"string","format":"date-time"}`)
	want(t, at(t, item, "site"), `{"type":"string","format":"uri"}`)
	want(t, at(t, item, "ip"), `{"type":"string","format":"ipv4"}`)
	if s.Defs == nil || len(s.Defs.Keys()) != 2 {
		t.Errorf("defs = %s", js(&Schema{Defs: s.Defs}))
	}

	// Too few records for anything to count as repeated.
	s = run(t, `{"status": "open"}`)
	want(t, at(t, s, "status"), `{"type":"string"}`)
	if s.Defs != nil {
		t.Errorf("unused defs: %s", js(s))
	}
}

func TestNumbers(t *testing.T) {
	s := run(t, `{"a": [1, 2, 99], "b": [-3, 0.5], "c": [-250, -7], "d": [0.002, 0.03],
		"e": [1e300], "pin": [1234, 5678], "f": [3.0]}`)
	want(t, at(t, s, "a", "[]"), `{"type":"integer","minimum":0,"maximum":100}`)
	want(t, at(t, s, "b", "[]"), `{"type":"number","minimum":-10,"maximum":1}`)
	want(t, at(t, s, "c", "[]"), `{"type":"integer","minimum":-1000,"maximum":0}`)
	want(t, at(t, s, "d", "[]"), `{"type":"number","minimum":0,"maximum":0.1}`)
	want(t, at(t, s, "e", "[]"), `{"type":"number","minimum":0,"maximum":1e300}`)
	want(t, at(t, s, "pin", "[]"), `{"type":"integer"}`)
	want(t, at(t, s, "f", "[]"), `{"type":"number","minimum":0,"maximum":10}`)
}

func TestArrays(t *testing.T) {
	s := run(t, `{"pairs": [["a", 1], ["b", 2], ["c", 3]], "mixed": [1, "a", 2, "b", 3],
		"points": [[1, 2, 3], [4, 5, 6], [7, 8, 9]], "one": ["x", 1, true], "empty": [[], []],
		"sizes": [[3, 50], [7, 20]]}`)
	want(t, at(t, s, "pairs", "[]"), `{"type":"array","prefixItems":[{"type":"string"},`+
		`{"type":"integer","minimum":0,"maximum":10}],"items":false,"minItems":2}`)
	want(t, at(t, s, "mixed"), `{"type":"array","items":{"type":["integer","string"],"minimum":0,"maximum":10}}`)
	want(t, at(t, s, "points"), `{"type":"array","items":{"type":"array","items":`+
		`{"type":"integer","minimum":0,"maximum":10},"minItems":3,"maxItems":3}}`)
	want(t, at(t, s, "one"), `{"type":"array","prefixItems":[{"type":"string"},{"type":"integer",`+
		`"minimum":0,"maximum":1},{"type":"boolean"}],"items":false,"minItems":3}`)
	want(t, at(t, s, "empty", "[]"), `{"description":"Only empty arrays were seen.","type":"array"}`)
	// Too few to say the positions really differ in size.
	want(t, at(t, s, "sizes", "[]"), `{"type":"array","items":{"type":"integer","minimum":0,"maximum":100}}`)

	// Enough to say so.
	s = run(t, records(30, func(i int) string { return fmt.Sprintf(`[%d, %d]`, i%9, i*30) }))
	want(t, at(t, s, "[]"), `{"type":"array","prefixItems":[{"type":"integer","minimum":0,"maximum":10},`+
		`{"type":"integer","minimum":0,"maximum":1000}],"items":false,"minItems":2}`)
}

func TestNullable(t *testing.T) {
	s := run(t, `[{"a": {"x": 1}, "b": "s", "c": null}, {"a": null, "b": null, "c": null}]`)
	want(t, at(t, s, "[]", "a"), `{"type":["object","null"],"properties":{"x":{"type":"integer",`+
		`"minimum":0,"maximum":1}},"required":["x"],"additionalProperties":false}`)
	want(t, at(t, s, "[]", "b"), `{"type":["string","null"]}`)
	want(t, at(t, s, "[]", "c"), `{"type":"null"}`)
}

func TestMaps(t *testing.T) {
	for name, src := range map[string]string{
		"uuid keys": `{"0b7c3a52-9a51-4a3f-8b44-3f0f2b8c1d11": 1, "6f1d6c1e-2b8a-4c5e-9d3f-0a1b2c3d4e5f": 2}`,
		"int keys":  `{"1": "a", "2": "b", "17": "c"}`,
		"token key": `{"name": "x", "` + randHex(20) + `": "y"}`,
		"records": `{"alice": {"uid": 1, "shell": "sh"}, "bob": {"uid": 2, "shell": "sh"},
			"carol": {"uid": 3, "shell": "zsh"}, "dan": {"uid": 4}, "eve": {"uid": 5, "shell": "sh"}}`,
	} {
		s := run(t, `{"m": `+src+`}`)
		m := at(t, s, "m")
		if m.Properties != nil || m.AdditionalProperties == false {
			t.Errorf("%s: not a map: %s", name, js(m))
		}
		var keys map[string]any
		json.Unmarshal([]byte(src), &keys)
		for k := range keys {
			if strings.Contains(js(s), `"`+k+`"`) {
				t.Errorf("%s: key %q leaked: %s", name, k, js(s))
			}
		}
	}
	// A config with a handful of differently-shaped sections is no map.
	s := run(t, `{"server": {"host": "h", "port": 1}, "db": {"url": "u"}, "log": {"level": "info"},
		"cache": {"size": 1}, "auth": {"issuer": "i", "client": "c"}}`)
	if s.Properties == nil {
		t.Errorf("config taken for a map: %s", js(s))
	}
}

func TestSeveralDocuments(t *testing.T) {
	var docs []any
	for _, src := range []string{`{"a": 1, "b": "x"}`, `{"a": 2}`} {
		d, _ := decode.Decode([]byte(src), decode.JSON, decode.Options{})
		docs = append(docs, d)
	}
	s := runDocs(t, docs...)
	if !reflect.DeepEqual(s.Required, []string{"a"}) {
		t.Errorf("required = %v", s.Required)
	}
}

// TestNoSecrets puts secrets everywhere a value could end up and checks
// none of them make it into the schema, even when they repeat.
func TestNoSecrets(t *testing.T) {
	secrets := []string{
		"gh" + "p_" + randB64(27),
		"AK" + "IA" + strings.ToUpper(randHex(8)),
		"eyJ" + randB64(20) + "." + randB64(40) + "." + randB64(32),
		"sk-" + randB64(36),
		randHex(32),
		randB64(30),
		"postgres://admin:" + "Sup3rS3cret" + "@db:5432/app",
		"hunter2",
		"-----BEGIN OPENSSH PRIVATE KEY-----\n" + randB64(60) + "\n-----END OPENSSH PRIVATE KEY-----",
		"4111 1111 1111 1111",
	}
	keys := []string{"value", "password", "apiKey", "x", "credentials"}
	var b strings.Builder
	b.WriteString("[")
	for i := range 400 {
		if i > 0 {
			b.WriteString(",")
		}
		sec := secrets[i%len(secrets)]
		q, _ := json.Marshal(sec)
		fmt.Fprintf(&b, `{%q: %s, "nested": {%q: [%s]}, "kind": "k%d", "list": [%s, 1], "m": {%s: 1}}`,
			keys[i%len(keys)], q, keys[(i+1)%len(keys)], q, i%2, q, q)
	}
	b.WriteString("]")
	out := js(run(t, b.String()))
	for _, sec := range append(secrets, "Sup3rS3cret") {
		if strings.Contains(out, sec) {
			t.Errorf("secret %q in schema:\n%s", sec, out)
		}
		if q, _ := json.Marshal(sec); strings.Contains(out, string(q[1:len(q)-1])) {
			t.Errorf("secret %q in schema:\n%s", sec, out)
		}
	}
}

func TestMagnitude(t *testing.T) {
	for _, c := range []struct {
		lit       string
		e         int
		neg, zero bool
	}{
		{"5", 1, false, false}, {"10", 1, false, false}, {"11", 2, false, false},
		{"1", 0, false, false}, {"0.5", 0, false, false}, {"0.05", -1, false, false},
		{"0.1", -1, false, false}, {"-5", 1, true, false}, {"0", 0, false, true},
		{"-0.0", 0, true, true}, {"65535", 5, false, false}, {"1e-9", -9, false, false},
		{"100000000000000001", 18, false, false}, {"1E+3", 3, false, false},
		{"12.5e-3", -1, false, false}, {"007", 1, false, false}, {"1e700", 700, false, false},
	} {
		e, neg, zero, ok := magnitude(c.lit)
		if !ok || zero != c.zero || !zero && (e != c.e || neg != c.neg) {
			t.Errorf("magnitude(%q) = %d, %v, %v, %v; want %d, %v, %v", c.lit, e, neg, zero, ok, c.e, c.neg, c.zero)
		}
	}
	for _, bad := range []string{"NaN", "+Inf", "-Inf", "1e99999", "", "-", "1.2.3"} {
		if _, _, _, ok := magnitude(bad); ok {
			t.Errorf("magnitude(%q) ok", bad)
		}
	}
	for e, want := range map[int]string{0: "1", 3: "1000", -1: "0.1", -6: "0.000001", -7: "1e-7", 21: "1e21"} {
		if got := pow10(e, false); string(got) != want {
			t.Errorf("pow10(%d) = %s, want %s", e, got, want)
		}
	}
}

func TestFormat(t *testing.T) {
	for s, want := range map[string]string{
		"2024-02-29": "date", "2023-02-29": "", "07:32:00Z": "time", "23:59:59.5+01:00": "time",
		"24:00:00Z": "", "07:32:00": "", "2024-01-02T03:04:05Z": "date-time",
		"2024-01-02t03:04:05.123-08:00": "date-time", "2024-01-02T25:04:05Z": "",
		"a.b+c@example.co.uk": "email", ".a@example.com": "", "a..b@example.com": "",
		strings.Repeat("a", 65) + "@example.com": "", "a@-x.com": "", "a@x.c": "",
		"https://example.com/a?b=c#d": "uri", "http://[::1]:80/": "uri", "http://::1/": "",
		"https://example.com/a b": "", "https://example.com/é": "", "https://x/%zz": "",
		"10.0.0.1": "ipv4", "010.0.0.1": "", "::1": "ipv6", "fe80::1%eth0": "",
		"0b7c3a52-9a51-4a3f-8b44-3f0f2b8c1d11": "uuid", "hello": "",
	} {
		if got := format(s); got != want {
			t.Errorf("format(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestHighEntropy(t *testing.T) {
	for _, s := range []string{
		randHex(16), randHex(32), randB64(24), randB64(48),
		"https://hooks.example.com/services/" + randB64(24),
		"Bearer " + randB64(30), "gh" + "o_" + randB64(30),
	} {
		if !highEntropy(s) {
			t.Errorf("highEntropy(%q) = false", s)
		}
	}
	for _, s := range []string{
		"hello world", "/usr/local/share/applications", "some_config_value_name",
		"https://example.com/path/to/the/thing", "2024-01-01T00:00:00Z", "python3.12-site-packages",
		"The quick brown fox jumps over the lazy dog", "abcdefghijklmnopqrstuvwxyz",
		"v1.2.3", "ERROR_CODE_404_NOT_FOUND", "TheQuickBrownFoxJumpsOverTheLazyDog", "getUserAccountForAdmin2", "UserAccountCreated",
		"MyApplicationConfiguration", "parseHTTPResponseHeaders", "en_US.UTF-8", "x86_64-linux-gnu", "1234567890123456", "user@example.com", "Mozilla/5.0 (X11; Linux x86_64)",
	} {
		if highEntropy(s) {
			t.Errorf("highEntropy(%q) = true", s)
		}
	}
}

func TestSensitiveName(t *testing.T) {
	for _, k := range []string{"password", "dbPassword", "API_KEY", "apiKey", "secret_access_key",
		"githubToken", "Authorization", "session_id", "privateKey", "client-secret", "x-api-key"} {
		if !sensitiveName(k) {
			t.Errorf("sensitiveName(%q) = false", k)
		}
	}
	for _, k := range []string{"name", "author", "keyboard", "status", "passenger", "bypass", "monkey", "cardinality"} {
		if sensitiveName(k) {
			t.Errorf("sensitiveName(%q) = true", k)
		}
	}
}
