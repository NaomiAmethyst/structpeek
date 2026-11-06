// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Keeping errors from giving away what they're about, and limits on hostile
// input. Go can't recover from a stack overflow, so the
// third-party parsers, which recurse without limit, must not be handed
// anything nested deeply enough to overflow theirs.

var errorWordRE = regexp.MustCompile(`[\pL\pN_]{2,}`)

// redact takes a parser's error and blanks out any word that's also in the
// input, as parsers like to quote what they tripped on. That's sometimes a
// secret, and an error goes where a schema's values wouldn't. Numbers small
// enough to be lines or columns stay, so the error still says where.
func redact(err error, data []byte) error {
	msg := errorWordRE.ReplaceAllStringFunc(err.Error(), func(w string) string {
		if n, numErr := strconv.Atoi(w); numErr == nil && n <= len(data) {
			return w
		}
		if bytes.Contains(data, []byte(w)) {
			return "…"
		}
		return w
	})
	return errors.New(msg)
}

// maxPlistNodes caps how many values a plist may expand to, as a binary
// plist's objects can be referred to many times over.
const maxPlistNodes = 10_000_000

// checkNesting returns an error if data nests more than maxDepth deep. It
// only looks at bytes, so brackets in strings count, but no real file has
// thousands of those unbalanced.
//
// With expr set, it allows for expressions, as in HCL, that recurse without
// brackets: a chain of operators (!!!!x, a?b:c?d:e, 1+1+1) or postfixes
// (a[b][c][d]) counts as nesting, up to a comma or the end of a statement.
func checkNesting(format string, data []byte, opens, closes string, expr bool) error {
	type level struct {
		open  byte
		chain int
	}
	var stack []level
	depth, chain := 0, 0 // the depth of the brackets, and the chain inside them
	for _, c := range data {
		switch {
		case strings.IndexByte(opens, c) >= 0:
			stack = append(stack, level{c, chain})
			depth += chain + 1
			chain = 0
		case strings.IndexByte(closes, c) >= 0:
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			depth -= top.chain + 1
			chain = top.chain
			if expr {
				chain++
			}
		case !expr:
			continue
		case c == ',':
			chain = 0
		case c == '\n':
			if len(stack) == 0 || stack[len(stack)-1].open == '{' {
				chain = 0
			}
		case strings.IndexByte("!-?+*/%&|<>", c) >= 0:
			chain++
		}
		if depth+chain > maxDepth {
			return fmt.Errorf("%s: nested too deeply", format)
		}
	}
	return nil
}

// checkPlist applies the limits to a plist, in whichever form it's in.
func checkPlist(data []byte) error {
	if bytes.HasPrefix(data, []byte("bplist")) {
		return checkBinaryPlist(data)
	}
	t := bytes.TrimLeft(data, " \t\r\n")
	if len(t) == 0 || t[0] != '<' {
		return checkNesting("plist", data, "({", ")}", false)
	}
	depth := 0
	for {
		i := bytes.IndexByte(t, '<')
		if i < 0 {
			return nil
		}
		t = t[i+1:]
		switch {
		case bytes.HasPrefix(t, []byte("/array")), bytes.HasPrefix(t, []byte("/dict")):
			depth--
		case bytes.HasPrefix(t, []byte("array")), bytes.HasPrefix(t, []byte("dict")):
			if end := bytes.IndexByte(t, '>'); end > 0 && t[end-1] == '/' {
				continue
			}
			if depth++; depth > maxDepth {
				return errors.New("plist: nested too deeply")
			}
		}
	}
}

// checkBinaryPlist works out how many values a binary plist expands to, and
// how deeply they nest, by walking its table of objects. The parser expands
// shared objects before anything else can look at them, so a small file can
// hold 2^40 values.
func checkBinaryPlist(data []byte) error {
	malformed := errors.New("plist: malformed binary plist")
	if len(data) < 8+32 {
		return malformed
	}
	trailer := data[len(data)-32:]
	offsetSize, refSize := uint64(trailer[6]), uint64(trailer[7])
	count := binary.BigEndian.Uint64(trailer[8:])
	top := binary.BigEndian.Uint64(trailer[16:])
	table := binary.BigEndian.Uint64(trailer[24:])
	size := uint64(len(data))
	if offsetSize == 0 || offsetSize > 8 || refSize == 0 || refSize > 8 || top >= count ||
		table >= size || count > (size-table)/offsetSize {
		return malformed
	}
	uintAt := func(at, n uint64) uint64 {
		var v uint64
		for _, b := range data[at : at+n] {
			v = v<<8 | uint64(b)
		}
		return v
	}

	// children returns where object i's references start, and how many there
	// are. Only arrays, sets and dictionaries have any.
	children := func(i uint64) (at, n uint64, err error) {
		at = uintAt(table+i*offsetSize, offsetSize)
		if at >= size {
			return 0, 0, malformed
		}
		marker := data[at]
		perItem := uint64(1)
		switch marker >> 4 {
		case 0xA, 0xC:
		case 0xD:
			perItem = 2
		default:
			return 0, 0, nil
		}
		at++
		n = uint64(marker & 0xF)
		if n == 0xF { // the count is an integer object of its own
			if at >= size || data[at]>>4 != 1 || data[at]&0xF > 3 {
				return 0, 0, malformed
			}
			width := uint64(1) << (data[at] & 0xF)
			if at+1+width > size {
				return 0, 0, malformed
			}
			n = uintAt(at+1, width)
			at += 1 + width
		}
		if n > (size-at)/refSize/perItem {
			return 0, 0, malformed
		}
		return at, n * perItem, nil
	}

	// expanded[i] is how many values object i expands to, once worked out.
	expanded := make(map[uint64]uint64)
	var walk func(i uint64, depth int) (uint64, error)
	walk = func(i uint64, depth int) (uint64, error) {
		if i >= count {
			return 0, malformed
		}
		if depth > maxDepth {
			return 0, errors.New("plist: nested too deeply")
		}
		if v, ok := expanded[i]; ok {
			if v == 0 { // still being worked out, so it contains itself
				return 0, malformed
			}
			return v, nil
		}
		at, n, err := children(i)
		if err != nil {
			return 0, err
		}
		expanded[i] = 0
		total := uint64(1)
		for j := range n {
			v, err := walk(uintAt(at+j*refSize, refSize), depth+1)
			if err != nil {
				return 0, err
			}
			if total += v; total > maxPlistNodes {
				return 0, errors.New("plist: too many values once references are followed")
			}
		}
		expanded[i] = total
		return total, nil
	}
	_, err := walk(top, 0)
	return err
}
