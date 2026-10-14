// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"encoding/json"
	"strings"
	"testing"

	"howett.net/plist"
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
	// Raw control characters, as broken files have, are let through.
	check(t, JSON, Options{}, "{\"a\": \"x\x14y\", \"b\": \"\\n\x01\"}", `{"a":"x\u0014y","b":"\n\u0001"}`)

	// A log saved with the JSON gets a hint.
	_, err := Decode([]byte("waiting...\n{oops\nstill waiting\n{\"a\": 1}\n"), JSON, Options{})
	if err == nil || !strings.Contains(err.Error(), "JSON starts at line 4") {
		t.Errorf("no hint where the JSON starts: %v", err)
	}
	_, err = Decode([]byte("not json\nat all\n"), JSON, Options{})
	if err == nil || strings.Contains(err.Error(), "starts at line") {
		t.Errorf("wrong hint: %v", err)
	}

	for _, bad := range []string{``, `{"a":`, `{} }`, `[1,,]`, `{"a" 1}`, `/* open`, `[.]`, `{1: 2}`} {
		if _, err := Decode([]byte(bad), JSON, Options{}); err == nil {
			t.Errorf("Decode(%q) succeeded", bad)
		}
	}
}

func TestJSON5(t *testing.T) {
	check(t, JSON, Options{}, `{
  // tsconfig.json style
  "compilerOptions": {"strict": true, "paths": {"@/*": ["src/*"],},},
  /* block */ "files": [],
}`, `{"compilerOptions":{"strict":true,"paths":{"@/*":["src/*"]}},"files":[]}`)
	check(t, JSON, Options{}, `{unquoted: 'single "q"', $id: 0x1F, _n: -.5, p: +3, t: 5., e: 1E3,
		inf: -Infinity, nan: NaN, s: 'a\
b\x41\u00e9\uD83D\uDE00\q', z: 007}`,
		`{"unquoted":"single \"q\"","$id":31,"_n":-0.5,"p":3,"t":5.0,"e":1E3,`+
			`"inf":-Inf,"nan":NaN,"s":"abAé😀q","z":7}`)
}

func TestEnv(t *testing.T) {
	check(t, Env, Options{}, `# credentials
AWS_ACCESS_KEY_ID="EXAMPLEKEYID"
export AWS_SECRET_ACCESS_KEY='lit$eral \n'
PORT=8080 # the port
DEBUG=false
EMPTY=
URL=https://example.com/#frag
MULTI="line one
line \"two\" \$HOME"
SPACED = ok
export ALSO="x" # comment
use nix
`, `{"AWS_ACCESS_KEY_ID":"EXAMPLEKEYID","AWS_SECRET_ACCESS_KEY":"lit$eral \\n","PORT":8080,`+
		`"DEBUG":false,"EMPTY":"","URL":"https://example.com/#frag","MULTI":"line one\nline \"two\" $HOME",`+
		`"SPACED":"ok","ALSO":"x"}`)
	for _, bad := range []string{"", "# only a comment\n", "A=\"unclosed\n", "A=\"x\" y\n"} {
		if _, err := Decode([]byte(bad), Env, Options{}); err == nil {
			t.Errorf("Decode(%q) succeeded", bad)
		}
	}
}

func TestProperties(t *testing.T) {
	check(t, Properties, Options{}, "# comment\n! also\nserver.port=8080\nspring.datasource.url : jdbc:x\n"+
		"greeting Hello \\\n    world\nescaped\\ key=caf\\u00e9\\tok\nempty\nflag = true\nserver.port=9090\n",
		`{"server.port":9090,"spring.datasource.url":"jdbc:x","greeting":"Hello world",`+
			`"escaped key":"café\tok","empty":"","flag":true}`)
}

func TestHCL(t *testing.T) {
	check(t, HCL, Options{}, `
# Terraform
region = "eu-west-2"
count  = 3
ratio  = 1.5
tags   = { Name = "web", "team-x" = true }
ports  = [80, 443]

resource "aws_instance" "web" {
  ami           = var.ami
  instance_type = "t3.micro"
  name          = "${local.prefix}-web"
  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_eip" "ip" {
  instance = aws_instance.web.id
}

locals {
  doc = <<EOT
hello
EOT
}
`, `{"region":"eu-west-2","count":3,"ratio":1.5,"tags":{"Name":"web","team-x":true},"ports":[80,443],`+
		`"resource":[{"$labels":["aws_instance","web"],"ami":"var.ami","instance_type":"t3.micro",`+
		`"name":"${local.prefix}-web","lifecycle":[{"create_before_destroy":true}]},`+
		`{"$labels":["aws_eip","ip"],"instance":"aws_instance.web.id"}],"locals":[{"doc":"hello\n"}]}`)
	if _, err := Decode([]byte("resource {"), HCL, Options{}); err == nil {
		t.Error("broken HCL decoded")
	}
}

func TestPlist(t *testing.T) {
	check(t, Plist, Options{}, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Name</key><string>x</string>
	<key>Count</key><integer>-3</integer>
	<key>Ratio</key><real>2</real>
	<key>On</key><true/>
	<key>When</key><date>2026-01-02T03:04:05Z</date>
	<key>Data</key><data>AQID</data>
	<key>List</key><array><string>a</string><dict/></array>
</dict>
</plist>`, `{"Count":-3,"Data":"AQID","List":["a",{}],"Name":"x","On":true,"Ratio":2.0,"When":"2026-01-02T03:04:05Z"}`)
	bin, err := plist.Marshal(map[string]any{"Port": 443, "Hosts": []string{"a"}, "Blob": []byte{1, 2, 3}}, plist.BinaryFormat)
	if err != nil {
		t.Fatal(err)
	}
	check(t, Plist, Options{}, string(bin), `{"Blob":"AQID","Hosts":["a"],"Port":443}`)
	check(t, Auto, Options{}, string(bin), `{"Blob":"AQID","Hosts":["a"],"Port":443}`)
	check(t, Plist, Options{}, "{ Name = x; List = (a, b); }", `{"List":["a","b"],"Name":"x"}`)
	if _, err := Decode([]byte("<plist><dict><key>x</key></plist>"), Plist, Options{}); err == nil {
		t.Error("broken plist decoded")
	}
}

func TestNetrc(t *testing.T) {
	check(t, Netrc, Options{}, `# comment
machine api.example.com login bot password "s3cret pass"
machine
  ftp.example.com
  login anonymous
  account 42
macdef init
cd /pub
binary

default login guest password guest
`, `[{"machine":"api.example.com","login":"bot","password":"s3cret pass"},`+
		`{"machine":"ftp.example.com","login":"anonymous","account":"42"},`+
		`{"macdef":"init","commands":"cd /pub\nbinary"},{"default":true,"login":"guest","password":"guest"}]`)
	for _, bad := range []string{"", "login x", "machine", "machine x login"} {
		if _, err := Decode([]byte(bad), Netrc, Options{}); err == nil {
			t.Errorf("Decode(%q) succeeded", bad)
		}
	}
}

func TestSSHConfig(t *testing.T) {
	check(t, SSHConfig, Options{}, `# global
Include ~/.ssh/config.d/*
ServerAliveInterval 60

Host github.com gist.github.com
    HostName github.com
    User git
    IdentityFile ~/.ssh/id_ed25519
    IdentityFile ~/.ssh/id_rsa
    IdentitiesOnly yes

Host *
  ForwardAgent=no
  LocalForward 8080 localhost:80

Match host *.internal exec "test -f /tmp/x"
  ProxyJump "bastion"
`, `{"include":"~/.ssh/config.d/*","serveraliveinterval":60,`+
		`"host":[{"$patterns":["github.com","gist.github.com"],"hostname":"github.com","user":"git",`+
		`"identityfile":["~/.ssh/id_ed25519","~/.ssh/id_rsa"],"identitiesonly":true},`+
		`{"$patterns":["*"],"forwardagent":false,"localforward":"8080 localhost:80"}],`+
		`"match":[{"$criteria":"host *.internal exec \"test -f /tmp/x\"","proxyjump":"bastion"}]}`)
	if _, err := Decode([]byte("Host\n"), SSHConfig, Options{}); err == nil {
		t.Error("keyword without a value decoded")
	}
}

func TestPgpass(t *testing.T) {
	check(t, Pgpass, Options{}, "# comment\ndb.example.com:5432:app:alice:pa\\:ss:word\n*:*:*:bob:x\n",
		`[{"hostname":"db.example.com","port":5432,"database":"app","username":"alice","password":"pa:ss:word"},`+
			`{"hostname":"*","port":"*","database":"*","username":"bob","password":"x"}]`)
	for _, bad := range []string{"", "a:b:c\n"} {
		if _, err := Decode([]byte(bad), Pgpass, Options{}); err == nil {
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
	check(t, Auto, Options{}, "export A=1\nB='x y'\n", `{"A":1,"B":"x y"}`)
	check(t, Auto, Options{}, "{\n  // c\n  a: 1,\n}\n", `{"a":1}`)
	check(t, Auto, Options{}, "resource \"x\" \"y\" {\n  n = 1\n}\n", `{"resource":[{"$labels":["x","y"],"n":1}]}`)
	check(t, Auto, Options{}, "<?xml version=\"1.0\"?>\n<plist version=\"1.0\"><dict><key>a</key><integer>1</integer></dict></plist>", `{"a":1}`)
}

func TestForPath(t *testing.T) {
	for name, want := range map[string]Format{
		"a.json": JSON, "a.JSONL": JSON, "b.yml": YAML, "c.toml": TOML,
		"d.conf": INI, "e.csv": CSV, "f.tsv": TSV, "i.svg": XML, "j.XML": XML,
		".env": Env, ".env.local": Env, "prod.env": Env, ".envrc": Env, ".env.json": JSON,
		"x.properties": Properties, "main.tf": HCL, "terraform.tfvars": HCL, "Info.plist": Plist,
		".netrc": Netrc, "_netrc": Netrc, "/home/u/.ssh/config": SSHConfig, "/etc/ssh/sshd_config": SSHConfig,
		"config": Auto, ".pgpass": Pgpass, "tsconfig.json": JSON, "a.jsonc": JSON, "b.json5": JSON, "g": Auto, "h.txt": Auto,
	} {
		if got := ForPath(name); got != want {
			t.Errorf("ForPath(%q) = %q, want %q", name, got, want)
		}
	}
}
