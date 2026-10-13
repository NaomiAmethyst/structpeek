// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
)

// decodeTOML reads a TOML document. Dates and times become strings.
func decodeTOML(data []byte) (any, error) {
	var m map[string]any
	md, err := toml.Decode(string(data), &m)
	if err != nil {
		return nil, fmt.Errorf("TOML: %w", err)
	}
	// The decoder hands back plain maps; its metadata remembers the order
	// keys came in.
	order := map[string]int{}
	for i, k := range md.Keys() {
		if _, ok := order[k.String()]; !ok {
			order[k.String()] = i
		}
	}
	return tomlValue(m, nil, order), nil
}

func tomlValue(v any, path toml.Key, order map[string]int) any {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		pos := make(map[string]int, len(v))
		for k := range v {
			keys = append(keys, k)
			p, ok := order[append(path[:len(path):len(path)], k).String()]
			if !ok {
				p = len(order)
			}
			pos[k] = p
		}
		sort.Slice(keys, func(i, j int) bool {
			if pos[keys[i]] != pos[keys[j]] {
				return pos[keys[i]] < pos[keys[j]]
			}
			return keys[i] < keys[j]
		})
		o := NewObject()
		for _, k := range keys {
			o.Set(k, tomlValue(v[k], append(path[:len(path):len(path)], k), order))
		}
		return o
	case []map[string]any:
		a := make([]any, len(v))
		for i, e := range v {
			a[i] = tomlValue(e, path, order)
		}
		return a
	case []any:
		a := make([]any, len(v))
		for i, e := range v {
			a[i] = tomlValue(e, path, order)
		}
		return a
	case int64:
		return json.Number(strconv.FormatInt(v, 10))
	case float64:
		return floatNumber(v)
	case bool, string:
		return v
	case time.Time:
		// The decoder marks dates and times without a zone with these
		// made-up zones.
		switch v.Location().String() {
		case "datetime-local":
			return v.Format("2006-01-02T15:04:05.999999999")
		case "date-local":
			return v.Format(time.DateOnly)
		case "time-local":
			return v.Format("15:04:05.999999999")
		}
		return v.Format(time.RFC3339Nano)
	}
	return fmt.Sprint(v)
}
