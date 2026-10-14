// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// maxDepth is how deeply JSON may nest.
const maxDepth = 10_000

// decodeJSON reads one JSON value, or a stream of them (JSON Lines), which
// becomes an array. It's a parser of its own, rather than encoding/json's
// token stream, because that's slow and objects have to keep their order.
func decodeJSON(data []byte) (any, error) {
	p := &jsonParser{data: data}
	var docs []any
	for p.skipSpace(); p.pos < len(p.data); p.skipSpace() {
		v, err := p.value(0)
		if err != nil {
			if len(docs) == 0 {
				if line := jsonStartsAt(data); line > 1 {
					return nil, fmt.Errorf("%w; it looks like JSON starts at line %d, after text that isn't JSON", err, line)
				}
			}
			return nil, err
		}
		docs = append(docs, v)
	}
	switch len(docs) {
	case 0:
		return nil, errors.New("JSON: no values")
	case 1:
		return docs[0], nil
	}
	return docs, nil
}

// jsonStartsAt looks for a line, after the first, from which the rest of
// data is JSON, as when a log's been saved along with it, and returns its
// number, or 0.
func jsonStartsAt(data []byte) int {
	tries := 0
	for i, line := 0, 1; i < len(data) && tries < 5; line++ {
		end := bytes.IndexByte(data[i:], '\n')
		if end < 0 {
			end = len(data) - i
		}
		if t := bytes.TrimLeft(data[i:i+end], " \t"); line > 1 && len(t) > 0 && (t[0] == '{' || t[0] == '[') {
			tries++
			if _, err := decodeJSON(data[i:]); err == nil {
				return line
			}
		}
		i += end + 1
	}
	return 0
}

type jsonParser struct {
	data []byte
	pos  int
}

func (p *jsonParser) errorf(format string, args ...any) error {
	line, col := 1, 1
	for _, c := range p.data[:min(p.pos, len(p.data))] {
		if c == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return fmt.Errorf("JSON: line %d, column %d: %s", line, col, fmt.Sprintf(format, args...))
}

func (p *jsonParser) skipSpace() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, p.errorf("nested too deeply")
	}
	p.skipSpace()
	if p.pos >= len(p.data) {
		return nil, p.errorf("unexpected end of input")
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		return p.string()
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	}
	for _, lit := range []struct {
		text string
		val  any
	}{{"true", true}, {"false", false}, {"null", nil}} {
		if len(p.data)-p.pos >= len(lit.text) && string(p.data[p.pos:p.pos+len(lit.text)]) == lit.text {
			p.pos += len(lit.text)
			return lit.val, nil
		}
	}
	return nil, p.errorf("unexpected %q", p.data[p.pos])
}

func (p *jsonParser) object(depth int) (any, error) {
	p.pos++ // {
	o := NewObject()
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == '}' {
		p.pos++
		return o, nil
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != '"' {
			return nil, p.errorf("expected a key")
		}
		k, err := p.string()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.pos >= len(p.data) || p.data[p.pos] != ':' {
			return nil, p.errorf("expected ':'")
		}
		p.pos++
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		o.Set(k, v)
		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, p.errorf("unexpected end of input")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return o, nil
		default:
			return nil, p.errorf("expected ',' or '}'")
		}
	}
}

func (p *jsonParser) array(depth int) (any, error) {
	p.pos++ // [
	a := []any{}
	p.skipSpace()
	if p.pos < len(p.data) && p.data[p.pos] == ']' {
		p.pos++
		return a, nil
	}
	for {
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		a = append(a, v)
		p.skipSpace()
		if p.pos >= len(p.data) {
			return nil, p.errorf("unexpected end of input")
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return a, nil
		default:
			return nil, p.errorf("expected ',' or ']'")
		}
	}
}

func (p *jsonParser) string() (string, error) {
	start := p.pos
	p.pos++ // "
	escaped := false
	for p.pos < len(p.data) {
		switch c := p.data[p.pos]; {
		case c == '"':
			p.pos++
			raw := p.data[start:p.pos]
			if !escaped && utf8.Valid(raw) {
				return string(raw[1 : len(raw)-1]), nil
			}
			var s string
			if err := json.Unmarshal(escapeControls(raw), &s); err != nil {
				p.pos = start
				return "", p.errorf("bad string: %v", err)
			}
			return s, nil
		case c == '\\':
			escaped = true
			p.pos += 2
		default:
			p.pos++
		}
	}
	return "", p.errorf("unterminated string")
}

// escapeControls escapes control characters in a JSON string, which JSON
// forbids raw but broken files have anyway: they're accepted, as Python's
// json does with strict=False, since no string from the data is written
// out.
func escapeControls(raw []byte) []byte {
	if !bytes.ContainsFunc(raw, func(r rune) bool { return r < 0x20 }) {
		return raw
	}
	out := make([]byte, 0, len(raw)+16)
	for _, c := range raw {
		if c < 0x20 {
			out = fmt.Appendf(out, `\u%04x`, c)
		} else {
			out = append(out, c)
		}
	}
	return out
}

func (p *jsonParser) number() (any, error) {
	start := p.pos
	digits := func() int {
		n := 0
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
			n++
		}
		return n
	}
	if p.data[p.pos] == '-' {
		p.pos++
	}
	if p.pos < len(p.data) && p.data[p.pos] == '0' {
		p.pos++
	} else if digits() == 0 {
		return nil, p.errorf("bad number")
	}
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		if digits() == 0 {
			return nil, p.errorf("bad number")
		}
	}
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if digits() == 0 {
			return nil, p.errorf("bad number")
		}
	}
	return json.Number(p.data[start:p.pos]), nil
}
