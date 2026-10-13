// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

// Package decode reads JSON, YAML, TOML, INI and CSV into one tree of plain
// values, the way JSON would see them: nil, bool, string, json.Number,
// []any and *Object. Objects keep their keys in document order.
package decode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
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
	Auto Format = ""
	JSON Format = "json"
	YAML Format = "yaml"
	TOML Format = "toml"
	INI  Format = "ini"
	CSV  Format = "csv"
	TSV  Format = "tsv"
)

// Formats lists the formats a user can ask for by name.
var Formats = []Format{JSON, YAML, TOML, INI, CSV, TSV}

// ParseFormat turns a name, as given on the command line, into a Format.
func ParseFormat(s string) (Format, error) {
	switch f := Format(strings.ToLower(s)); f {
	case Auto, JSON, YAML, TOML, INI, CSV, TSV:
		return f, nil
	case "yml":
		return YAML, nil
	case "jsonl", "ndjson":
		return JSON, nil
	}
	return Auto, fmt.Errorf("unknown format %q", s)
}

// ForPath guesses a file's format from its name, or returns Auto.
func ForPath(name string) Format {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json", ".jsonl", ".ndjson", ".geojson", ".jsonld":
		return JSON
	case ".yaml", ".yml":
		return YAML
	case ".toml":
		return TOML
	case ".ini", ".cfg", ".conf", ".cnf", ".properties", ".desktop":
		return INI
	case ".csv":
		return CSV
	case ".tsv", ".tab":
		return TSV
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

// Decode reads data in format f. With Auto it tries JSON, then YAML.
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
	case Auto:
		if v, err := decodeJSON(data); err == nil {
			return v, nil
		}
		v, err := decodeYAML(data)
		if err != nil {
			return nil, fmt.Errorf("neither JSON nor YAML (try --format): %w", err)
		}
		return v, nil
	}
	return nil, fmt.Errorf("unknown format %q", f)
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
