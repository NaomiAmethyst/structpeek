// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strings"
)

// Readers for the small formats credentials are kept in.

// decodeNetrc reads a .netrc file into an array of its entries, in order:
// {"machine": ..., "login": ..., "password": ..., "account": ...}, or
// {"default": true, ...}, or {"macdef": name, "commands": ...} for a macro.
// Every value is a string. Tokens may be quoted, and # starts a comment.
func decodeNetrc(data []byte) (any, error) {
	t := &netrcTokens{data: data}
	entries := []any{}
	var cur *Object
	for {
		tok, ok, err := t.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		switch tok {
		case "machine", "macdef":
			name, ok, err := t.next()
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("netrc: %s without a name", tok)
			}
			o := NewObject()
			o.Set(tok, name)
			entries = append(entries, o)
			cur = o
			if tok == "macdef" {
				o.Set("commands", t.macro())
				cur = nil
			}
		case "default":
			cur = NewObject()
			cur.Set("default", true)
			entries = append(entries, cur)
		default:
			if cur == nil {
				return nil, fmt.Errorf("netrc: %q outside a machine entry", tok)
			}
			val, ok, err := t.next()
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("netrc: %s without a value", tok)
			}
			cur.Set(tok, val)
		}
	}
	if len(entries) == 0 {
		return nil, errors.New("netrc: no entries")
	}
	return entries, nil
}

type netrcTokens struct {
	data []byte
	pos  int
}

func (t *netrcTokens) next() (string, bool, error) {
	for t.pos < len(t.data) {
		switch c := t.data[t.pos]; {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			t.pos++
		case c == '#':
			if end := bytes.IndexByte(t.data[t.pos:], '\n'); end >= 0 {
				t.pos += end
			} else {
				t.pos = len(t.data)
			}
		case c == '"':
			var b strings.Builder
			for t.pos++; t.pos < len(t.data); t.pos++ {
				switch c := t.data[t.pos]; {
				case c == '"':
					t.pos++
					return b.String(), true, nil
				case c == '\\' && t.pos+1 < len(t.data):
					t.pos++
					b.WriteByte(t.data[t.pos])
				default:
					b.WriteByte(c)
				}
			}
			return "", false, errors.New("netrc: unclosed quote")
		default:
			start := t.pos
			for t.pos < len(t.data) && !strings.ContainsRune(" \t\r\n", rune(t.data[t.pos])) {
				t.pos++
			}
			return string(t.data[start:t.pos]), true, nil
		}
	}
	return "", false, nil
}

// macro reads a macdef's commands: the lines after it, up to a blank one.
func (t *netrcTokens) macro() string {
	if end := bytes.IndexByte(t.data[t.pos:], '\n'); end >= 0 {
		t.pos += end + 1
	} else {
		t.pos = len(t.data)
	}
	start := t.pos
	end := bytes.Index(t.data[t.pos:], []byte("\n\n"))
	if end < 0 {
		t.pos = len(t.data)
		return strings.TrimRight(string(t.data[start:]), "\n")
	}
	t.pos += end + 2
	return string(t.data[start : start+end])
}

// decodeSSHConfig reads an OpenSSH client or server config (ssh_config,
// ~/.ssh/config, sshd_config) into an object. Keywords, which ssh treats
// alike in any case, are written in lower case, as ssh -G does. Settings
// before the first Host or Match are at the top level; each Host block is
// an object in "host", with its patterns under "$patterns", and each Match
// block one in "match", with its criteria under "$criteria". A keyword
// given more than once in a block, as IdentityFile often is, is an array.
// Values read as bools (yes, no), numbers or strings.
func decodeSSHConfig(data []byte) (any, error) {
	root := NewObject()
	cur := root
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 16<<20)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || text[0] == '#' {
			continue
		}
		end := strings.IndexAny(text, " \t=")
		if end < 0 {
			return nil, fmt.Errorf("ssh config: line %d: %q has no value", line, text)
		}
		keyword := strings.ToLower(text[:end])
		args := strings.TrimLeft(text[end:], " \t")
		args = strings.TrimSpace(strings.TrimPrefix(args, "="))
		if len(args) >= 2 && args[0] == '"' && args[len(args)-1] == '"' && !strings.Contains(args[1:len(args)-1], `"`) {
			args = args[1 : len(args)-1]
		}
		switch keyword {
		case "host", "match":
			block := NewObject()
			if keyword == "host" {
				var patterns []any
				for _, p := range strings.Fields(args) {
					patterns = append(patterns, strings.Trim(p, `"`))
				}
				block.Set("$patterns", patterns)
			} else {
				block.Set("$criteria", args)
			}
			prev, _ := root.Get(keyword)
			list, _ := prev.([]any)
			root.Set(keyword, append(list, block))
			cur = block
			continue
		}
		val := scalar(args)
		switch prev, ok := cur.Get(keyword); {
		case !ok:
			cur.Set(keyword, val)
		default:
			if list, isList := prev.([]any); isList {
				cur.Set(keyword, append(list, val))
			} else {
				cur.Set(keyword, []any{prev, val})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("ssh config: %w", err)
	}
	return root, nil
}

// pgpassFields are the fields of each line of a .pgpass file.
var pgpassFields = []string{"hostname", "port", "database", "username", "password"}

// decodePgpass reads a PostgreSQL .pgpass file into an array of objects,
// one per line, with hostname, port, database, username and password.
// Fields are split at colons; \: and \\ are escapes. The port reads as a
// number unless it's "*"; the rest are strings.
func decodePgpass(data []byte) (any, error) {
	entries := []any{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 16<<20)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(text) == "" || strings.HasPrefix(strings.TrimSpace(text), "#") {
			continue
		}
		var fields []string
		var b strings.Builder
		for i := 0; i < len(text); i++ {
			switch c := text[i]; {
			case c == '\\' && i+1 < len(text):
				i++
				b.WriteByte(text[i])
			case c == ':' && len(fields) < len(pgpassFields)-1:
				fields = append(fields, b.String())
				b.Reset()
			default:
				b.WriteByte(c)
			}
		}
		fields = append(fields, b.String())
		if len(fields) != len(pgpassFields) {
			return nil, fmt.Errorf("pgpass: line %d: want %d fields separated by colons, got %d", line, len(pgpassFields), len(fields))
		}
		o := NewObject()
		for i, name := range pgpassFields {
			var v any = fields[i]
			if name == "port" {
				v = scalar(fields[i])
			}
			o.Set(name, v)
		}
		entries = append(entries, o)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("pgpass: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("pgpass: no entries")
	}
	return entries, nil
}
