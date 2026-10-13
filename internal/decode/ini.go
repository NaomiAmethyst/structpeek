// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// decodeINI reads an INI file into an object of sections, each an object
// of keys. Keys before the first section sit at the top level. A key given
// more than once (or written key[]) becomes an array; a key with no value
// is true.
func decodeINI(data []byte) (any, error) {
	root := NewObject()
	cur := root
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 16<<20)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || text[0] == ';' || text[0] == '#' {
			continue
		}
		if text[0] == '[' {
			end := strings.LastIndexByte(text, ']')
			if end < 0 {
				return nil, fmt.Errorf("INI: line %d: unclosed section", line)
			}
			name := strings.TrimSpace(text[1:end])
			prev, _ := root.Get(name)
			sec, ok := prev.(*Object)
			if !ok {
				sec = NewObject()
				root.Set(name, sec)
			}
			cur = sec
			continue
		}
		key, val := text, any(true)
		if i := strings.IndexAny(text, "=:"); i >= 0 {
			key = strings.TrimSpace(text[:i])
			val = iniValue(strings.TrimSpace(text[i+1:]))
		}
		list := strings.HasSuffix(key, "[]")
		key = strings.TrimSuffix(key, "[]")
		if key == "" {
			return nil, fmt.Errorf("INI: line %d: no key", line)
		}
		switch prev, ok := cur.Get(key); {
		case !ok && list:
			cur.Set(key, []any{val})
		case !ok:
			cur.Set(key, val)
		default:
			if a, isList := prev.([]any); isList {
				cur.Set(key, append(a, val))
			} else {
				cur.Set(key, []any{prev, val})
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("INI: %w", err)
	}
	return root, nil
}

func iniValue(s string) any {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	// A comment after the value, as long as there's space before it.
	for i := 1; i < len(s); i++ {
		if (s[i] == ';' || s[i] == '#') && (s[i-1] == ' ' || s[i-1] == '\t') {
			s = strings.TrimSpace(s[:i])
			break
		}
	}
	return scalar(s)
}
