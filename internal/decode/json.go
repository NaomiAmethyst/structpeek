// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// maxDepth is how deeply JSON may nest.
const maxDepth = 10_000

// decodeJSON reads one JSON value, or a stream of them (JSON Lines), which
// becomes an array. It reads JSON5 too, and so JSONC (JSON with comments,
// as in tsconfig.json): comments, trailing commas, unquoted keys, single
// quotes, hex numbers, Infinity and NaN and the rest, and raw control
// characters in strings, which broken files have. It's a parser of its
// own, rather than encoding/json's token stream, because that's slow,
// strict, and loses the order of objects' keys.
func decodeJSON(data []byte) (any, error) {
	v, err := parseJSON(data)
	if err != nil {
		if line := jsonStartsAt(data); line > 1 {
			return nil, fmt.Errorf("%w; it looks like JSON starts at line %d, after text that isn't JSON", err, line)
		}
	}
	return v, err
}

func parseJSON(data []byte) (any, error) {
	p := &jsonParser{data: data}
	var docs []any
	for {
		if err := p.skipSpace(); err != nil {
			return nil, err
		}
		if p.pos >= len(p.data) {
			break
		}
		v, err := p.value(0)
		if err != nil {
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
			if _, err := parseJSON(data[i:]); err == nil {
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

// skipSpace skips whitespace and comments.
func (p *jsonParser) skipSpace() error {
	for p.pos < len(p.data) {
		switch c := p.data[p.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f':
			p.pos++
		case c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '/':
			end := bytes.IndexByte(p.data[p.pos:], '\n')
			if end < 0 {
				end = len(p.data) - p.pos
			}
			p.pos += end
		case c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '*':
			end := bytes.Index(p.data[p.pos+2:], []byte("*/"))
			if end < 0 {
				return p.errorf("unclosed comment")
			}
			p.pos += end + 4
		case c >= utf8.RuneSelf:
			r, n := utf8.DecodeRune(p.data[p.pos:])
			if !unicode.IsSpace(r) && r != '\uFEFF' {
				return nil
			}
			p.pos += n
		default:
			return nil
		}
	}
	return nil
}

// next skips space and comments, and returns the next byte, or 0 at the
// end.
func (p *jsonParser) next() (byte, error) {
	if err := p.skipSpace(); err != nil {
		return 0, err
	}
	if p.pos >= len(p.data) {
		return 0, nil
	}
	return p.data[p.pos], nil
}

func (p *jsonParser) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, p.errorf("nested too deeply")
	}
	c, err := p.next()
	if err != nil {
		return nil, err
	}
	switch {
	case p.pos >= len(p.data):
		return nil, p.errorf("unexpected end of input")
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"' || c == '\'':
		return p.string()
	case c == '-' || c == '+' || c == '.' || c >= '0' && c <= '9':
		return p.number()
	}
	for _, lit := range []struct {
		text string
		val  any
	}{{"true", true}, {"false", false}, {"null", nil}, {"Infinity", json.Number("+Inf")}, {"NaN", json.Number("NaN")}} {
		if bytes.HasPrefix(p.data[p.pos:], []byte(lit.text)) {
			p.pos += len(lit.text)
			return lit.val, nil
		}
	}
	return nil, p.errorf("unexpected %q", c)
}

func (p *jsonParser) object(depth int) (any, error) {
	p.pos++ // {
	o := NewObject()
	for {
		c, err := p.next()
		if err != nil {
			return nil, err
		}
		if c == '}' {
			p.pos++
			return o, nil
		}
		var k string
		switch {
		case p.pos >= len(p.data):
			err = p.errorf("unexpected end of input")
		case c == '"' || c == '\'':
			k, err = p.string()
		case c == '_' || c == '$' || c >= utf8.RuneSelf || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z':
			k, err = p.identifier()
		default:
			err = p.errorf("expected a key")
		}
		if err != nil {
			return nil, err
		}
		if c, err = p.next(); err != nil {
			return nil, err
		} else if c != ':' {
			return nil, p.errorf("expected ':'")
		}
		p.pos++
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		o.Set(k, v)
		switch c, err = p.next(); {
		case err != nil:
			return nil, err
		case p.pos >= len(p.data):
			return nil, p.errorf("unexpected end of input")
		case c == ',':
			p.pos++
		case c == '}':
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
	for {
		c, err := p.next()
		if err != nil {
			return nil, err
		}
		if c == ']' {
			p.pos++
			return a, nil
		}
		v, err := p.value(depth + 1)
		if err != nil {
			return nil, err
		}
		a = append(a, v)
		switch c, err = p.next(); {
		case err != nil:
			return nil, err
		case p.pos >= len(p.data):
			return nil, p.errorf("unexpected end of input")
		case c == ',':
			p.pos++
		case c == ']':
			p.pos++
			return a, nil
		default:
			return nil, p.errorf("expected ',' or ']'")
		}
	}
}

// identifier reads an unquoted JSON5 key.
func (p *jsonParser) identifier() (string, error) {
	start := p.pos
	for p.pos < len(p.data) {
		r, n := utf8.DecodeRune(p.data[p.pos:])
		if r != '_' && r != '$' && !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.Is(unicode.Mn, r) {
			break
		}
		p.pos += n
	}
	if p.pos == start {
		return "", p.errorf("expected a key")
	}
	return string(p.data[start:p.pos]), nil
}

func (p *jsonParser) string() (string, error) {
	quote := p.data[p.pos]
	start := p.pos
	p.pos++
	escaped := false
	for p.pos < len(p.data) {
		switch c := p.data[p.pos]; c {
		case quote:
			p.pos++
			body := p.data[start+1 : p.pos-1]
			if !escaped {
				return strings.ToValidUTF8(string(body), "\uFFFD"), nil
			}
			return unescape(body), nil
		case '\\':
			escaped = true
			p.pos += 2
		default:
			p.pos++
		}
	}
	p.pos = start
	return "", p.errorf("unterminated string")
}

// unescape undoes JSON and JSON5 escapes: \n and the like, \xHH, \uHHHH
// (in surrogate pairs too), and backslash-newline, which continues a
// string on the next line. Anything else escaped stands for itself.
func unescape(b []byte) string {
	var out strings.Builder
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' || i+1 == len(b) {
			out.WriteByte(b[i])
			continue
		}
		i++
		switch c := b[i]; c {
		case 'n':
			out.WriteByte('\n')
		case 't':
			out.WriteByte('\t')
		case 'r':
			out.WriteByte('\r')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'v':
			out.WriteByte('\v')
		case '0':
			out.WriteByte(0)
		case '\n':
		case '\r':
			if i+1 < len(b) && b[i+1] == '\n' {
				i++
			}
		case 'x':
			if r, ok := hexRune(b[i+1:], 2); ok {
				out.WriteRune(r)
				i += 2
			} else {
				out.WriteByte(c)
			}
		case 'u':
			r, ok := hexRune(b[i+1:], 4)
			if !ok {
				out.WriteByte(c)
				continue
			}
			i += 4
			if utf16.IsSurrogate(r) && i+6 < len(b) && b[i+1] == '\\' && b[i+2] == 'u' {
				if r2, ok := hexRune(b[i+3:], 4); ok {
					if pair := utf16.DecodeRune(r, r2); pair != utf8.RuneError {
						r = pair
						i += 6
					}
				}
			}
			out.WriteRune(r)
		default:
			out.WriteByte(c)
		}
	}
	return strings.ToValidUTF8(out.String(), "\uFFFD")
}

// hexRune reads n hex digits from the start of b.
func hexRune(b []byte, n int) (rune, bool) {
	if len(b) < n {
		return 0, false
	}
	var r rune
	for _, c := range b[:n] {
		switch {
		case c >= '0' && c <= '9':
			r = r*16 + rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r*16 + rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r*16 + rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return r, true
}

// number reads a JSON or JSON5 number, and writes it as a json.Number
// that JSON would accept: +1 as 1, .5 as 0.5, 5. as 5.0, hex in decimal,
// and infinities and NaN as YAML's are, "+Inf", "-Inf" and "NaN".
func (p *jsonParser) number() (any, error) {
	start := p.pos
	sign := ""
	if c := p.data[p.pos]; c == '-' || c == '+' {
		if c == '-' {
			sign = "-"
		}
		p.pos++
	}
	rest := p.data[p.pos:]
	switch {
	case bytes.HasPrefix(rest, []byte("Infinity")):
		p.pos += len("Infinity")
		if sign == "-" {
			return json.Number("-Inf"), nil
		}
		return json.Number("+Inf"), nil
	case bytes.HasPrefix(rest, []byte("NaN")):
		p.pos += len("NaN")
		return json.Number("NaN"), nil
	case len(rest) > 2 && rest[0] == '0' && (rest[1] == 'x' || rest[1] == 'X'):
		p.pos += 2
		h := p.pos
		for p.pos < len(p.data) && strings.IndexByte("0123456789abcdefABCDEF", p.data[p.pos]) >= 0 {
			p.pos++
		}
		n, ok := new(big.Int).SetString(string(p.data[h:p.pos]), 16)
		if !ok {
			return nil, p.errorf("bad number")
		}
		return json.Number(sign + n.String()), nil
	}
	digits := func() string {
		s := p.pos
		for p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9' {
			p.pos++
		}
		return string(p.data[s:p.pos])
	}
	whole := digits()
	frac, hasFrac := "", false
	if p.pos < len(p.data) && p.data[p.pos] == '.' {
		p.pos++
		frac, hasFrac = digits(), true
	}
	if whole == "" && frac == "" {
		p.pos = start
		return nil, p.errorf("bad number")
	}
	exp := ""
	if p.pos < len(p.data) && (p.data[p.pos] == 'e' || p.data[p.pos] == 'E') {
		e := p.pos
		p.pos++
		if p.pos < len(p.data) && (p.data[p.pos] == '+' || p.data[p.pos] == '-') {
			p.pos++
		}
		if digits() == "" {
			return nil, p.errorf("bad number")
		}
		exp = string(p.data[e:p.pos])
	}
	// JSON forbids leading zeros; reading them anyway does no harm.
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	n := sign + whole
	if hasFrac {
		if frac == "" {
			frac = "0"
		}
		n += "." + frac
	}
	return json.Number(n + exp), nil
}
