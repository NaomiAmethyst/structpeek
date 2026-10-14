// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"howett.net/plist"
)

// decodePlist reads an Apple property list, in XML, binary or the old
// text form, into the values it holds: dictionaries become objects (keys
// sorted, as plists don't keep their order), dates RFC 3339 strings, and
// data base64 strings.
func decodePlist(data []byte) (any, error) {
	var v any
	if _, err := plist.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("plist: %w", err)
	}
	return plistValue(v), nil
}

func plistValue(v any) any {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		o := NewObject()
		for _, k := range keys {
			o.Set(k, plistValue(v[k]))
		}
		return o
	case []any:
		a := make([]any, len(v))
		for i, e := range v {
			a[i] = plistValue(e)
		}
		return a
	case string, bool:
		return v
	case uint64:
		return json.Number(strconv.FormatUint(v, 10))
	case int64:
		return json.Number(strconv.FormatInt(v, 10))
	case float64:
		return floatNumber(v)
	case time.Time:
		return v.UTC().Format(time.RFC3339)
	case []byte:
		return base64.StdEncoding.EncodeToString(v)
	case plist.UID:
		return json.Number(strconv.FormatUint(uint64(v), 10))
	case nil:
		return nil
	}
	return fmt.Sprint(v)
}
