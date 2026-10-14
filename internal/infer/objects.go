// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package infer

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/NaomiAmethyst/structpeek/internal/decode"
)

// objects infers the schemas for the objects seen in one place: one map,
// or one schema per kind of object.
func (in *inferrer) objects(objs []*decode.Object, p place) []*Schema {
	p = namesSecret(objs, p)
	if in.isMap(objs) {
		return []*Schema{in.mapSchema(objs, p)}
	}
	if oneKeyEach(objs) {
		return []*Schema{in.oneOfKeys(objs, p)}
	}
	kinds := in.cluster(objs)
	if len(kinds) > maxKinds {
		kinds = [][]*decode.Object{objs}
	}
	out := make([]*Schema, len(kinds))
	for i, k := range kinds {
		out[i] = in.object(k, p)
	}
	if len(kinds) > 1 {
		in.tag(kinds, out, p)
	}
	return out
}

// object infers one closed object schema that fits all of objs.
func (in *inferrer) object(objs []*decode.Object, p place) *Schema {
	var keys []string
	var vals [][]any // vals[i] holds the values of keys[i]
	index := map[string]int{}
	for _, o := range objs {
		for i, k := range o.Keys {
			j, ok := index[k]
			if !ok {
				j = len(keys)
				index[k] = j
				keys = append(keys, k)
				vals = append(vals, nil)
			}
			vals[j] = append(vals[j], o.Values[i])
		}
	}
	s := &Schema{Type: "object"}
	if len(keys) == 0 {
		s.Description = "Only empty objects were seen."
		return s
	}
	s.Properties = &Properties{}
	for i, k := range keys {
		s.Properties.Set(k, in.infer(vals[i], p.child(k)))
		if len(vals[i]) == len(objs) {
			s.Required = append(s.Required, k)
		}
	}
	s.AdditionalProperties = false
	return s
}

// namesSecret marks a place as secret if any of its objects names a
// secret under a name-like key, as name/value lists do: {"name":
// "DB_PASSWORD", "value": "..."}, or <param name="api_key" value="..."/>
// in XML. Values are pooled across the objects, so one such name makes all
// of them secret.
func namesSecret(objs []*decode.Object, p place) place {
	if p.sensitive {
		return p
	}
	for _, o := range objs {
		for i, k := range o.Keys {
			s, ok := o.Values[i].(string)
			if ok && nameKeys[strings.ToLower(k)] && identRE.MatchString(s) && sensitiveName(s) {
				return place{sensitive: true}
			}
		}
	}
	return p
}

// nameKeys are keys that, in a name/value list, hold the name.
var nameKeys = map[string]bool{
	"name": true, "key": true, "id": true, "field": true, "param": true, "parameter": true,
	"variable": true, "var": true, "env": true, "setting": true, "option": true,
	"property": true, "attribute": true, "label": true, "@name": true, "@key": true,
}

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:-]{0,63}$`)

// oneKeyEach reports whether every object has exactly one key, and not
// all the same one: a choice of one of several keys, like a list of XML
// elements or a union tagged by its key.
func oneKeyEach(objs []*decode.Object) bool {
	for _, o := range objs {
		if len(o.Keys) != 1 {
			return false
		}
	}
	for _, o := range objs[1:] {
		if o.Keys[0] != objs[0].Keys[0] {
			return true
		}
	}
	return false
}

// oneOfKeys infers one schema for objects that each have one of several
// keys: all of the keys are allowed, and exactly one is required.
func (in *inferrer) oneOfKeys(objs []*decode.Object, p place) *Schema {
	s := in.object(objs, p)
	one := 1
	s.Required, s.MinProperties, s.MaxProperties = nil, &one, &one
	return s
}

// cluster sorts objects into kinds. Objects with the same keys are one
// kind to start with; then, biggest first, each kind joins the existing
// kind its keys are most like, if they're alike enough, or starts a new
// one. Empty objects join the biggest kind. Objects keep their order
// within each kind.
func (in *inferrer) cluster(objs []*decode.Object) [][]*decode.Object {
	type kind struct {
		idx  []int
		keys map[string]bool
	}
	bySig := map[string]*kind{}
	var groups []*kind
	for i, o := range objs {
		keys := slices.Clone(o.Keys)
		sort.Strings(keys)
		sig := strings.Join(keys, "\x00")
		g, ok := bySig[sig]
		if !ok {
			g = &kind{keys: map[string]bool{}}
			for _, k := range keys {
				g.keys[k] = true
			}
			bySig[sig] = g
			groups = append(groups, g)
		}
		g.idx = append(g.idx, i)
	}
	sort.SliceStable(groups, func(i, j int) bool { return len(groups[i].idx) > len(groups[j].idx) })

	var kinds []*kind
	var empty []int
	for _, g := range groups {
		if len(g.keys) == 0 {
			empty = g.idx
			continue
		}
		best, bestSim := -1, 0.0
		for i, k := range kinds {
			if sim := jaccard(g.keys, k.keys); sim >= in.opts.Similarity && sim > bestSim {
				best, bestSim = i, sim
			}
		}
		if best < 0 {
			kinds = append(kinds, g)
			continue
		}
		k := kinds[best]
		k.idx = append(k.idx, g.idx...)
		for key := range g.keys {
			k.keys[key] = true
		}
	}
	if len(kinds) == 0 {
		return [][]*decode.Object{objs}
	}
	kinds[0].idx = append(kinds[0].idx, empty...)

	out := make([][]*decode.Object, len(kinds))
	for i, k := range kinds {
		sort.Ints(k.idx)
		for _, j := range k.idx {
			out[i] = append(out[i], objs[j])
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

func jaccard(a, b map[string]bool) float64 {
	both := 0
	for k := range a {
		if b[k] {
			both++
		}
	}
	return float64(both) / float64(len(a)+len(b)-both)
}

var tagRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:/-]{0,39}$`)

// tag looks for a field that tells kinds of object apart: one every kind
// requires, holding the same short name throughout each kind and a
// different one in each. If there is one, each kind's schema gets its
// value as a const. The values are shown just as an enum's would be, so
// they have to pass the same tests, repetition included.
func (in *inferrer) tag(kinds [][]*decode.Object, schemas []*Schema, p place) {
	n := 0
	for _, objs := range kinds {
		n += len(objs)
	}
	if p.sensitive || float64(len(kinds)) >= in.opts.Cardinality*float64(n) {
		return
	}
candidates:
	for _, k := range schemas[0].Required {
		if sensitiveName(k) {
			continue
		}
		values := make([]string, len(kinds))
		seen := map[string]bool{}
		for i, objs := range kinds {
			if !slices.Contains(schemas[i].Required, k) {
				continue candidates
			}
			for j, o := range objs {
				val, _ := o.Get(k)
				v, ok := val.(string)
				if !ok || j > 0 && v != values[i] {
					continue candidates
				}
				values[i] = v
			}
			v := values[i]
			if seen[v] || !tagRE.MatchString(v) {
				continue candidates
			}
			seen[v] = true
		}
		if !in.enumSafe(values, p.child(k)) {
			continue
		}
		for i, s := range schemas {
			s.Properties.Set(k, &Schema{Type: "string", Const: &values[i]})
		}
		return
	}
}

// isMap reports whether objects are being used as maps, with data for
// keys rather than field names. That's so if any key looks like a token,
// half of them look like data, there are too many keys to be field names,
// many objects share few of their keys, or the values are all objects of
// much the same kind under enough different keys.
func (in *inferrer) isMap(objs []*decode.Object) bool {
	count := map[string]int{}
	total := 0
	for _, o := range objs {
		for _, k := range o.Keys {
			count[k]++
			total++
		}
	}
	if len(count) == 0 {
		return false
	}
	data := 0
	for k := range count {
		if highEntropy(k) {
			return true
		}
		if dataKey(k) {
			data++
		}
	}
	switch {
	case data*2 >= len(count), len(count) > in.opts.MaxProperties:
		return true
	case len(objs) >= 4 && len(count) >= 16 && float64(total) < 0.2*float64(len(count)*len(objs)):
		return true
	case len(count) < minRecordMap:
		return false
	}
	union := map[string]bool{}
	sizes := 0
	for _, o := range objs {
		for _, val := range o.Values {
			v, ok := val.(*decode.Object)
			if !ok || len(v.Keys) == 0 {
				return false
			}
			sizes += len(v.Keys)
			for _, vk := range v.Keys {
				union[vk] = true
			}
		}
	}
	return len(union) >= 2 && float64(sizes) >= 0.6*float64(total*len(union))
}

// mapSchema infers the schema for objects used as maps: their values are
// pooled, and their keys described only by what they look like.
func (in *inferrer) mapSchema(objs []*decode.Object, p place) *Schema {
	var vals []any
	var keys []string
	seen := map[string]bool{}
	for _, o := range objs {
		for i, k := range o.Keys {
			vals = append(vals, o.Values[i])
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
				// The values are pooled, so one secret key makes them all
				// secret.
				p.sensitive = p.sensitive || sensitiveName(k)
			}
		}
	}
	s := &Schema{
		Type:                 "object",
		Description:          "A map whose keys are data rather than field names, so the keys are withheld.",
		AdditionalProperties: in.infer(vals, p),
	}
	allInt, high := true, false
	for _, k := range keys {
		allInt = allInt && intRE.MatchString(k)
		high = high || highEntropy(k)
	}
	if allInt {
		s.PropertyNames = &Schema{Pattern: "^-?[0-9]+$"}
	} else if f := commonFormat(keys); f != "" {
		s.PropertyNames = &Schema{Format: f}
	} else if high {
		s.PropertyNames = ref(defHighEntropy)
	}
	return s
}
