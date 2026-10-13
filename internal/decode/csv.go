// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
)

// decodeCSV reads a CSV (or TSV) file. With a header row it becomes an
// array of objects, one per row, keyed by column name; without one, an
// array of arrays. Empty cells are null, and cells read as bools, numbers
// or strings.
func decodeCSV(data []byte, comma rune, header Header) (any, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = comma
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV: %w", err)
	}
	out := make([]any, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	if header == HeaderYes || header != HeaderNo && looksLikeHeader(rows) {
		names := columnNames(rows[0])
		for _, row := range rows[1:] {
			o := NewObject()
			for i, cell := range row {
				name := fmt.Sprintf("column_%d", i+1)
				if i < len(names) {
					name = names[i]
				}
				o.Set(name, csvCell(cell))
			}
			out = append(out, o)
		}
		return out, nil
	}
	for _, row := range rows {
		a := make([]any, len(row))
		for i, cell := range row {
			a[i] = csvCell(cell)
		}
		out = append(out, a)
	}
	return out, nil
}

func csvCell(s string) any {
	if s == "" {
		return nil
	}
	return scalar(s)
}

// columnNames makes header cells usable as keys: blank ones get a name
// and repeats get a number.
func columnNames(row []string) []string {
	names := make([]string, len(row))
	seen := map[string]bool{}
	for i, n := range row {
		if n == "" {
			n = "column_" + strconv.Itoa(i+1)
		}
		base := n
		for j := 2; seen[n]; j++ {
			n = base + "_" + strconv.Itoa(j)
		}
		seen[n] = true
		names[i] = n
	}
	return names
}

// looksLikeHeader guesses whether the first row names the columns. It has
// to be all distinct, non-empty text; then each column votes: one holding
// numbers or bools under a text heading says yes, and so does one whose
// heading never turns up among its values, while a heading repeated in
// its own column says no.
func looksLikeHeader(rows [][]string) bool {
	if len(rows) < 2 {
		return false
	}
	seen := map[string]bool{}
	for _, h := range rows[0] {
		if _, isText := scalar(h).(string); h == "" || !isText || seen[h] {
			return false
		}
		seen[h] = true
	}
	score := 0
	for i, h := range rows[0] {
		typed, text, repeated := 0, 0, false
		for _, row := range rows[1:] {
			if i >= len(row) || row[i] == "" {
				continue
			}
			if _, isText := scalar(row[i]).(string); isText {
				text++
			} else {
				typed++
			}
			repeated = repeated || row[i] == h
		}
		switch {
		case typed > 0 && text == 0:
			score++
		case repeated:
			score--
		case text > 0:
			score++
		}
	}
	return score > 0
}
