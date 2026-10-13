// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"encoding/json"
	"testing"
)

// render writes a tree as JSON with object keys in their kept order, so
// tests can compare order as well as content.
func render(v any) string {
	switch v := v.(type) {
	case *Object:
		s := "{"
		for i, k := range v.Keys {
			if i > 0 {
				s += ","
			}
			b, _ := json.Marshal(k)
			s += string(b) + ":" + render(v.Values[i])
		}
		return s + "}"
	case []any:
		s := "["
		for i, e := range v {
			if i > 0 {
				s += ","
			}
			s += render(e)
		}
		return s + "]"
	case json.Number:
		return string(v)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func check(t *testing.T, f Format, o Options, in, want string) {
	t.Helper()
	v, err := Decode([]byte(in), f, o)
	if err != nil {
		t.Fatalf("Decode(%s): %v", f, err)
	}
	if got := render(v); got != want {
		t.Errorf("Decode(%s):\n got %s\nwant %s", f, got, want)
	}
}

func TestJSON(t *testing.T) {
	check(t, JSON, Options{}, `{"z": 1, "a": [1.0, "x", null, true], "m": {"b": 2, "a": 1}}`,
		`{"z":1,"a":[1.0,"x",null,true],"m":{"b":2,"a":1}}`)
	check(t, JSON, Options{}, "{\"a\":1}\n{\"a\":2}\n", `[{"a":1},{"a":2}]`)
	check(t, JSON, Options{}, "\xef\xbb\xbf[]", `[]`)
	for _, bad := range []string{``, `{"a":`, `{} }`, `[1,]`} {
		if _, err := Decode([]byte(bad), JSON, Options{}); err == nil {
			t.Errorf("Decode(%q) succeeded", bad)
		}
	}
}

func TestYAML(t *testing.T) {
	check(t, YAML, Options{}, `
base: &base
  host: localhost
  port: 5432
prod:
  <<: *base
  host: db.example.com
n: [1, 1.0, 0x10, .inf, ~, yes, "yes", 2024-01-02]
`, `{"base":{"host":"localhost","port":5432},"prod":{"host":"db.example.com","port":5432},`+
		`"n":[1,1.0,16,+Inf,null,"yes","yes","2024-01-02"]}`)
	check(t, YAML, Options{}, "a: 1\n---\na: 2\n", `[{"a":1},{"a":2}]`)
	check(t, YAML, Options{}, "---\n- a\n", `["a"]`)
	check(t, YAML, Options{}, "? [a, b]\n: 1\n", `{"[\"a\",\"b\"]":1}`)
}

func TestYAMLBillionLaughs(t *testing.T) {
	src := "a: &a [x, x, x, x, x, x, x, x, x, x]\n"
	prev := "a"
	for _, n := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		src += n + ": &" + n + " [*" + prev
		for range 9 {
			src += ", *" + prev
		}
		src += "]\n"
		prev = n
	}
	if _, err := Decode([]byte(src), YAML, Options{}); err == nil {
		t.Error("billion laughs decoded")
	}
}

func TestTOML(t *testing.T) {
	check(t, TOML, Options{}, `
title = "x"
when = 1979-05-27T07:32:00Z
local = 1979-05-27T07:32:00
at = 07:32:00
day = 1979-05-27
ratio = 2.0

[server]
port = 8080
host = "h"

[[fruit]]
name = "apple"
[[fruit]]
name = "pear"
colour = "green"
`, `{"title":"x","when":"1979-05-27T07:32:00Z","local":"1979-05-27T07:32:00","at":"07:32:00","day":"1979-05-27","ratio":2.0,`+
		`"server":{"port":8080,"host":"h"},"fruit":[{"name":"apple"},{"name":"pear","colour":"green"}]}`)
}

func TestINI(t *testing.T) {
	check(t, INI, Options{}, `
; comment
global = 1
[server]
host = example.com   ; where
port: 8080
debug = off
name = "quoted ; not a comment"
skip-name-resolve
[list]
item = a
item = b
one[] = 1
[server]
zero = 007
`, `{"global":1,"server":{"host":"example.com","port":8080,"debug":false,`+
		`"name":"quoted ; not a comment","skip-name-resolve":true,"zero":"007"},`+
		`"list":{"item":["a","b"],"one":[1]}}`)
}

func TestCSV(t *testing.T) {
	check(t, CSV, Options{}, "name,age,admin\nalice,30,true\nbob,,false\n",
		`[{"name":"alice","age":30,"admin":true},{"name":"bob","age":null,"admin":false}]`)
	check(t, CSV, Options{}, "1,2\n3,4\n", `[[1,2],[3,4]]`)
	check(t, CSV, Options{CSVHeader: HeaderNo}, "a,b\nc,d\n", `[["a","b"],["c","d"]]`)
	check(t, CSV, Options{CSVHeader: HeaderYes}, "a,a,\nc,d,e,f\n",
		`[{"a":"c","a_2":"d","column_3":"e","column_4":"f"}]`)
	check(t, TSV, Options{}, "k\tv\nx\t1\n", `[{"k":"x","v":1}]`)
	// The first row repeats in its column, so it's data.
	check(t, CSV, Options{}, "red,1\nred,2\n", `[["red",1],["red",2]]`)
	check(t, CSV, Options{CSVHeader: HeaderYes}, "", `[]`)
}

func TestXML(t *testing.T) {
	check(t, XML, Options{}, `<foos a="1" b="2" c="3">
  <foo>
    <a>1</a>
    <b>2</b>
    <c>3</c>
  </foo>
  <foo>
    <a>4</a>
    <b>5</b>
    <c>6</c>
  </foo>
</foos>`, `[{"foos":{"a":1,"b":2,"c":3,"$inner":[`+
		`{"foo":{"$inner":[{"a":{"$inner":1}},{"b":{"$inner":2}},{"c":{"$inner":3}}]}},`+
		`{"foo":{"$inner":[{"a":{"$inner":4}},{"b":{"$inner":5}},{"c":{"$inner":6}}]}}]}}]`)

	check(t, XML, Options{}, `<?xml version="1.0"?>
<!DOCTYPE doc>
<x:doc xmlns:x="urn:x" on="yes"><!-- hi -->
  <p>Hello <b>world</b>!</p>
  <q>one <!-- split --> line &amp; <![CDATA[<raw>]]></q>
  <br/>
  <empty>   </empty>
  <x:one><only/></x:one>
</x:doc>`, `[{"x:doc":{"xmlns:x":"urn:x","on":true,"$inner":[`+
		`{"p":{"$inner":["Hello",{"b":{"$inner":"world"}},"!"]}},`+
		`{"q":{"$inner":"one  line \u0026 \u003craw\u003e"}},{"br":{}},{"empty":{}},`+
		`{"x:one":{"$inner":[{"only":{}}]}}]}}]`)

	latin1 := append([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><n>caf`), 0xe9, '<', '/', 'n', '>')
	check(t, XML, Options{}, string(latin1), `[{"n":{"$inner":"café"}}]`)

	for _, bad := range []string{``, `<a>`, `<a></b>`, `<a></a></a>`, `just text`} {
		if _, err := Decode([]byte(bad), XML, Options{}); err == nil {
			t.Errorf("Decode(%q) succeeded", bad)
		}
	}
}

func TestAuto(t *testing.T) {
	check(t, Auto, Options{}, `{"a": 1}`, `{"a":1}`)
	check(t, Auto, Options{}, "a: 1\n", `{"a":1}`)
	check(t, Auto, Options{}, "\n <a>1</a>", `[{"a":{"$inner":1}}]`)
	check(t, Auto, Options{}, "# c\nname = \"x\"\n[s]\nport = 1\n", `{"name":"x","s":{"port":1}}`)
	check(t, Auto, Options{}, "[s]\nhost = example.com\nflag = on\n", `{"s":{"host":"example.com","flag":true}}`)
	check(t, Auto, Options{}, "name,n\nx,1\n", `[{"name":"x","n":1}]`)
	check(t, Auto, Options{}, "a\t1\nb\t2\n", `[["a",1],["b",2]]`)
	check(t, Auto, Options{}, "just some text\n", `"just some text"`)
	check(t, Auto, Options{}, "- a\n- b\n", `["a","b"]`)
}

func TestForPath(t *testing.T) {
	for name, want := range map[string]Format{
		"a.json": JSON, "a.JSONL": JSON, "b.yml": YAML, "c.toml": TOML,
		"d.conf": INI, "e.csv": CSV, "f.tsv": TSV, "i.svg": XML, "j.XML": XML, "g": Auto, "h.txt": Auto,
	} {
		if got := ForPath(name); got != want {
			t.Errorf("ForPath(%q) = %q, want %q", name, got, want)
		}
	}
}
