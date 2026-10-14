// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"strings"
	"unicode/utf8"
)

// decodeProperties reads a Java .properties file into an object of keys,
// as java.util.Properties does: # and ! start comments; a key ends at an
// unescaped "=", ":" or space; a line ending in a backslash goes on to the
// next; \t, \n, \uXXXX and the like are escapes. Keys stay as they are,
// dots and all, and values read as bools, numbers or strings. A key given
// twice keeps its last value.
func decodeProperties(data []byte) (any, error) {
	o := NewObject()
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimLeft(lines[i], " \t\f")
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		// Join continued lines: an odd number of backslashes at the end.
		for continues(line) && i+1 < len(lines) {
			i++
			line = line[:len(line)-1] + strings.TrimLeft(lines[i], " \t\f")
		}
		if continues(line) {
			line = line[:len(line)-1]
		}
		key, value := splitProperty(line)
		o.Set(unescapeProperty(key), scalar(unescapeProperty(value)))
	}
	return o, nil
}

func continues(line string) bool {
	n := len(line) - len(strings.TrimRight(line, "\\"))
	return n%2 == 1
}

// splitProperty splits a logical line into its key and value.
func splitProperty(line string) (key, value string) {
	end := len(line)
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if strings.IndexByte("=: \t\f", line[i]) >= 0 {
			end = i
			break
		}
	}
	key, rest := line[:end], strings.TrimLeft(line[end:], " \t\f")
	if rest != "" && (rest[0] == '=' || rest[0] == ':') {
		rest = strings.TrimLeft(rest[1:], " \t\f")
	}
	return key, rest
}

func unescapeProperty(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 'f':
			b.WriteByte('\f')
		case 'u':
			if r, ok := hexRune([]byte(s[i+1:]), 4); ok {
				b.WriteRune(r)
				i += 4
			} else {
				b.WriteByte('u')
			}
		default:
			b.WriteByte(s[i])
		}
	}
	return strings.ToValidUTF8(b.String(), string(utf8.RuneError))
}
