// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

// Package decode reads JSON, YAML, TOML, INI and CSV into one tree of plain
// values, the way JSON would see them: nil, bool, string, json.Number,
// []any and *Object. Objects keep their keys in document order.
package decode

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Object is a JSON object that remembers the order its keys came in.
type Object struct {
	Keys   []string
	Values []any          // Values[i] goes with Keys[i]
	index  map[string]int // built once there are enough keys to need it
}

// indexAt is how many keys an object has before it's worth indexing them.
const indexAt = 16

// NewObject returns an empty Object.
func NewObject() *Object {
	return &Object{}
}

func (o *Object) find(k string) int {
	if o.index != nil {
		if i, ok := o.index[k]; ok {
			return i
		}
		return -1
	}
	for i, ok := range o.Keys {
		if ok == k {
			return i
		}
	}
	return -1
}

// Set sets k to v. A new key goes on the end; an existing one keeps its
// place.
func (o *Object) Set(k string, v any) {
	if i := o.find(k); i >= 0 {
		o.Values[i] = v
		return
	}
	o.Keys = append(o.Keys, k)
	o.Values = append(o.Values, v)
	switch {
	case o.index != nil:
		o.index[k] = len(o.Keys) - 1
	case len(o.Keys) > indexAt:
		o.index = make(map[string]int, len(o.Keys))
		for i, k := range o.Keys {
			o.index[k] = i
		}
	}
}

// Get returns the value for k and whether it is there.
func (o *Object) Get(k string) (any, bool) {
	if i := o.find(k); i >= 0 {
		return o.Values[i], true
	}
	return nil, false
}

// Format is an input format.
type Format string

const (
	Auto       Format = ""
	JSON       Format = "json" // and JSON Lines, JSONC and JSON5
	YAML       Format = "yaml"
	TOML       Format = "toml"
	INI        Format = "ini"
	Env        Format = "env"
	Properties Format = "properties"
	CSV        Format = "csv"
	TSV        Format = "tsv"
	XML        Format = "xml"
	HCL        Format = "hcl"
	Plist      Format = "plist"
	Netrc      Format = "netrc"
	SSHConfig  Format = "ssh-config"
	Pgpass     Format = "pgpass"
)

// Formats lists the formats a user can ask for by name.
var Formats = []Format{JSON, YAML, TOML, INI, Env, Properties, CSV, TSV, XML, HCL, Plist, Netrc, SSHConfig, Pgpass}

// ParseFormat turns a name, as given on the command line, into a Format.
func ParseFormat(s string) (Format, error) {
	f := Format(strings.ToLower(s))
	if f == Auto || slices.Contains(Formats, f) {
		return f, nil
	}
	switch f {
	case "yml":
		return YAML, nil
	case "jsonl", "ndjson", "jsonc", "json5":
		return JSON, nil
	case "dotenv":
		return Env, nil
	case "terraform", "tf":
		return HCL, nil
	case "ssh", "ssh_config", "sshd_config":
		return SSHConfig, nil
	}
	return Auto, fmt.Errorf("unknown format %q", s)
}

// ForPath guesses a file's format from its name, or returns Auto.
func ForPath(name string) Format {
	base := strings.ToLower(filepath.Base(name))
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json", ".jsonl", ".ndjson", ".geojson", ".jsonld", ".jsonc", ".json5":
		return JSON
	case ".yaml", ".yml":
		return YAML
	case ".toml":
		return TOML
	case ".ini", ".cfg", ".conf", ".cnf", ".desktop":
		return INI
	case ".env", ".envrc":
		return Env
	case ".properties":
		return Properties
	case ".csv":
		return CSV
	case ".tsv", ".tab":
		return TSV
	case ".xml", ".xsd", ".xsl", ".xslt", ".svg", ".rss", ".atom", ".xhtml":
		return XML
	case ".hcl", ".tf", ".tfvars", ".nomad":
		return HCL
	case ".plist":
		return Plist
	case ".netrc":
		return Netrc
	case ".pgpass":
		return Pgpass
	}
	switch {
	case strings.HasPrefix(base, ".env."): // .env.local, .env.production
		return Env
	case base == "_netrc":
		return Netrc
	case base == "pgpass.conf":
		return Pgpass
	case base == "ssh_config", base == "sshd_config",
		base == "config" && filepath.Base(filepath.Dir(name)) == ".ssh":
		return SSHConfig
	}
	return Auto
}

// Header says whether a CSV file's first row names its columns.
type Header string

const (
	HeaderAuto Header = "auto"
	HeaderYes  Header = "yes"
	HeaderNo   Header = "no"
)

// Options tune decoding.
type Options struct {
	// CSVHeader says whether CSV and TSV files start with a header row.
	CSVHeader Header
}

// Decode reads data in format f, or, with Auto, whatever format it seems
// to be in.
func Decode(data []byte, f Format, o Options) (any, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	switch f {
	case JSON:
		return decodeJSON(data)
	case YAML:
		return decodeYAML(data)
	case TOML:
		return decodeTOML(data)
	case INI:
		return decodeINI(data)
	case CSV:
		return decodeCSV(data, ',', o.CSVHeader)
	case TSV:
		return decodeCSV(data, '\t', o.CSVHeader)
	case XML:
		return decodeXML(data)
	case Env:
		return decodeEnv(data, false)
	case Properties:
		return decodeProperties(data)
	case HCL:
		return decodeHCL(data)
	case Plist:
		return decodePlist(data)
	case Netrc:
		return decodeNetrc(data)
	case SSHConfig:
		return decodeSSHConfig(data)
	case Pgpass:
		return decodePgpass(data)
	case Auto:
		return decodeAuto(data, o)
	}
	return nil, fmt.Errorf("unknown format %q", f)
}

// decodeAuto works out the format by trying each in turn, most particular
// first: a binary plist (by its magic number); XML, or a plist, if it
// starts with "<"; JSON (or JSON5); shell-style KEY=value lines; TOML;
// YAML, if it gives more than a lone value, as nearly any text is a YAML
// string; HCL; INI; CSV or TSV, if every row has as many fields, and more
// than one; and at last YAML after all.
func decodeAuto(data []byte, o Options) (any, error) {
	if bytes.HasPrefix(data, []byte("bplist")) {
		return decodePlist(data)
	}
	if t := bytes.TrimLeft(data, " \t\r\n"); len(t) > 0 && t[0] == '<' {
		if head := t[:min(len(t), 1024)]; bytes.Contains(head, []byte("<plist")) {
			return decodePlist(data)
		}
		return decodeXML(data)
	}
	if v, err := parseJSON(data); err == nil {
		return v, nil
	}
	if v, err := decodeEnv(data, true); err == nil {
		return v, nil
	}
	if v, err := decodeTOML(data); err == nil && len(v.(*Object).Keys) > 0 {
		return v, nil
	}
	y, yerr := decodeYAML(data)
	if yerr == nil {
		switch y.(type) {
		case *Object, []any:
			return y, nil
		}
	}
	if v, err := decodeHCL(data); err == nil && len(v.(*Object).Keys) > 0 {
		return v, nil
	}
	if iniLike(data) {
		if v, err := decodeINI(data); err == nil {
			return v, nil
		}
	}
	for _, comma := range []rune{'\t', ','} {
		r := csv.NewReader(bytes.NewReader(data))
		r.Comma = comma
		if rows, err := r.ReadAll(); err == nil && len(rows) > 0 && len(rows[0]) > 1 {
			return decodeCSV(data, comma, o.CSVHeader)
		}
	}
	if yerr != nil {
		return nil, fmt.Errorf("can't tell what format this is (try --format): %w", yerr)
	}
	return y, nil
}

// iniLike reports whether every line is blank, a comment, a [section] or a
// key=value.
func iniLike(data []byte) bool {
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || line[0] == ';' || line[0] == '#':
		case line[0] == '[' && strings.HasSuffix(line, "]"), strings.Contains(line, "="):
			found = true
		default:
			return false
		}
	}
	return found
}

var jsonNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// scalar reads an untyped text value, as found in INI and CSV files, as a
// bool, number or string.
func scalar(s string) any {
	switch strings.ToLower(s) {
	case "true", "yes", "on":
		return true
	case "false", "no", "off":
		return false
	}
	if jsonNumber.MatchString(s) {
		return json.Number(s)
	}
	return s
}

// floatNumber writes f as a json.Number that still reads as a float, so
// 1.0 stays 1.0 rather than becoming the integer 1.
func floatNumber(f float64) json.Number {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "+Inf"
	case math.IsInf(f, -1):
		return "-Inf"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return json.Number(s)
}

// Plain turns a tree into the values encoding/json would have produced,
// with map[string]any for objects. Numbers stay json.Number.
func Plain(v any) any {
	switch v := v.(type) {
	case *Object:
		m := make(map[string]any, len(v.Keys))
		for i, k := range v.Keys {
			m[k] = Plain(v.Values[i])
		}
		return m
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = Plain(e)
		}
		return out
	}
	return v
}
