// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var envKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// decodeEnv reads a .env file, or shell variable assignments, into an
// object of variables: KEY=value, perhaps after "export". Values in single
// quotes are as written; in double quotes, backslash escapes work; either
// may run over several lines. Unquoted values read as bools, numbers or
// strings, up to a comment. Nothing is expanded: $VAR stays as it is.
//
// Strictly, as when guessing the format, every line must be an assignment
// with nothing around the "="; otherwise lines that aren't, like the shell
// commands in an .envrc, are skipped.
func decodeEnv(data []byte, strict bool) (any, error) {
	o := NewObject()
	s := &envScanner{data: data, line: 1}
	for s.pos < len(s.data) {
		start := s.line
		text := strings.TrimSpace(s.restOfLine())
		if text == "" || text[0] == '#' {
			s.skipLine()
			continue
		}
		if rest, ok := strings.CutPrefix(text, "export"); ok && rest != "" && (rest[0] == ' ' || rest[0] == '\t') {
			text = strings.TrimLeft(rest, " \t")
		}
		key, _, found := strings.Cut(text, "=")
		trimmed := strings.TrimSpace(key)
		if !found || !envKeyRE.MatchString(trimmed) || strict && trimmed != key {
			if strict {
				return nil, fmt.Errorf("env: line %d: not a KEY=value assignment", start)
			}
			s.skipLine()
			continue
		}
		// Move to just after the "=", and read the value from there.
		s.pos += bytes.IndexByte(s.data[s.pos:], '=') + 1
		if strict && s.pos < len(s.data) && (s.data[s.pos] == ' ' || s.data[s.pos] == '\t') {
			return nil, fmt.Errorf("env: line %d: space after '='", start)
		}
		v, err := s.value()
		if err != nil {
			return nil, fmt.Errorf("env: line %d: %w", start, err)
		}
		o.Set(trimmed, v)
	}
	if len(o.Keys) == 0 {
		return nil, errors.New("env: no KEY=value assignments")
	}
	return o, nil
}

type envScanner struct {
	data []byte
	pos  int
	line int
}

func (s *envScanner) restOfLine() string {
	end := bytes.IndexByte(s.data[s.pos:], '\n')
	if end < 0 {
		return string(s.data[s.pos:])
	}
	return string(s.data[s.pos : s.pos+end])
}

func (s *envScanner) skipLine() {
	end := bytes.IndexByte(s.data[s.pos:], '\n')
	if end < 0 {
		s.pos = len(s.data)
		return
	}
	s.pos += end + 1
	s.line++
}

// value reads a value, and what follows it to the end of its line.
func (s *envScanner) value() (any, error) {
	for s.pos < len(s.data) && (s.data[s.pos] == ' ' || s.data[s.pos] == '\t') {
		s.pos++
	}
	if s.pos >= len(s.data) || (s.data[s.pos] != '"' && s.data[s.pos] != '\'' && s.data[s.pos] != '`') {
		text := s.restOfLine()
		s.skipLine()
		for i := 0; i < len(text); i++ {
			if text[i] == '#' && (i == 0 || text[i-1] == ' ' || text[i-1] == '\t') {
				text = text[:i]
				break
			}
		}
		return scalar(strings.TrimSpace(strings.TrimSuffix(text, "\r"))), nil
	}
	quote := s.data[s.pos]
	s.pos++
	var b strings.Builder
	for ; s.pos < len(s.data); s.pos++ {
		c := s.data[s.pos]
		if c == '\n' {
			s.line++
		}
		switch {
		case c == quote:
			s.pos++
			rest := strings.TrimSpace(s.restOfLine())
			if rest != "" && rest[0] != '#' {
				return nil, fmt.Errorf("unexpected %q after closing quote", rest)
			}
			s.skipLine()
			return b.String(), nil
		case c == '\\' && quote == '"' && s.pos+1 < len(s.data):
			s.pos++
			switch e := s.data[s.pos]; e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\n':
				s.line++ // a continued line
			case '"', '\\', '$', '`':
				b.WriteByte(e)
			default:
				b.WriteByte('\\')
				b.WriteByte(e)
			}
		default:
			b.WriteByte(c)
		}
	}
	return nil, errors.New("unclosed quote")
}
