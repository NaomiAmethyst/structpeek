// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

// Package infer works out a JSON Schema that describes the shape of some
// data without giving away its values.
//
// Every value seen at one place in the data (each record's "name", every
// element of an array, the same key across several files) is an
// observation, and a place's schema is inferred from all its observations
// together:
//
//   - Objects are closed, and keys every observation has are required.
//     Objects whose key sets overlap enough are one kind, widened to fit
//     (keys some lack become optional, keys with mixed values get each
//     kind); objects too different to merge stay separate kinds in an
//     anyOf, told apart by a constant tag field if they have one. Objects
//     whose keys are data (IDs, numbers, addresses, tokens, or simply too
//     many to be field names) are maps, and their keys are withheld.
//   - Strings get a format if they all share one. Otherwise strings with
//     few distinct values for how often they're seen are low-cardinality:
//     an enum if every value is safe to show, else a reference to
//     $defs/lowCardinalityString. Strings that look random are a
//     reference to $defs/highEntropyString, and the rest are just strings.
//   - Numbers are integers if they're all written as integers, with a
//     range rounded out to powers of ten.
//   - Arrays of the same length whose positions hold different things are
//     tuples; otherwise their elements are pooled into one item schema.
//
// Nothing that could be a secret is ever written: only enums and tags
// carry values from the data, and only for short, repeated, non-random
// values under keys that don't sound sensitive.
package infer

import (
	"encoding/json"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/NaomiAmethyst/structpeek/internal/decode"
)

// Options tune inference.
type Options struct {
	// Cardinality is the fraction of a string's observations below which
	// its count of distinct values makes it low-cardinality.
	Cardinality float64
	// MaxEnum is the most values an enum may list.
	MaxEnum int
	// Similarity is how alike (by Jaccard index of their keys) two kinds
	// of object must be to merge into one.
	Similarity float64
	// MaxProperties is the most distinct keys an object may have before
	// it's taken to be a map.
	MaxProperties int
}

// DefaultOptions returns the options structpeek uses unless told
// otherwise.
func DefaultOptions() Options {
	return Options{Cardinality: 0.05, MaxEnum: 32, Similarity: 0.5, MaxProperties: 200}
}

const (
	defLowCardinality = "lowCardinalityString"
	defHighEntropy    = "highEntropyString"

	// maxKinds is the most kinds of object one place can have before
	// they're all merged into one: past that it's just varied.
	maxKinds = 8
	// maxTuple is the longest array taken for a tuple, and maxLoneTuple
	// the longest when it's the only array seen in its place.
	maxTuple     = 16
	maxLoneTuple = 4
	// minRangedTuple is how many arrays it takes for numbers of different
	// sizes in each position to make them tuples.
	minRangedTuple = 20
	// minRecordMap is how many keys an object of like objects needs
	// before it's taken for a map of records.
	minRecordMap = 5
	// maxEnumLen is the longest string an enum may list.
	maxEnumLen = 64
	// entropySample is how many distinct strings are checked for looking
	// random.
	entropySample = 1000
)

var defs = []struct{ name, description string }{
	{defLowCardinality, "A string drawn from a small set of values that repeat. The values are withheld, as they could be sensitive."},
	{defHighEntropy, "A string that looks randomly generated, such as a key, token, hash or opaque ID. The values are withheld."},
}

type inferrer struct {
	opts Options
}

// place says what's known about where in the data some observations are.
type place struct {
	sensitive bool // whether any key above them sounds secret
}

func (p place) child(key string) place {
	return place{p.sensitive || sensitiveName(key)}
}

// Infer returns a schema that each of docs fits.
func Infer(docs []any, o Options) *Schema {
	in := &inferrer{opts: o}
	root := *in.infer(docs, place{})
	root.Schema = Draft
	used := map[string]bool{}
	walk(&root, func(s *Schema) {
		if name, ok := strings.CutPrefix(s.Ref, "#/$defs/"); ok {
			used[name] = true
		}
	})
	for _, d := range defs {
		if used[d.name] {
			if root.Defs == nil {
				root.Defs = &Properties{}
			}
			root.Defs.Set(d.name, &Schema{Type: "string", Description: d.description})
		}
	}
	return &root
}

func walk(s *Schema, f func(*Schema)) {
	f(s)
	if s.Properties != nil {
		for _, k := range s.Properties.keys {
			walk(s.Properties.m[k], f)
		}
	}
	for _, sub := range []any{s.PropertyNames, s.AdditionalProperties, s.Items} {
		if sub, ok := sub.(*Schema); ok && sub != nil {
			walk(sub, f)
		}
	}
	for _, sub := range s.PrefixItems {
		walk(sub, f)
	}
	for _, sub := range s.AnyOf {
		walk(sub, f)
	}
}

func ref(def string) *Schema {
	return &Schema{Ref: "#/$defs/" + def}
}

func (in *inferrer) infer(vals []any, p place) *Schema {
	if len(vals) == 0 {
		return &Schema{}
	}
	var nObjs, nArrs, nStrs, nNums int
	for _, v := range vals {
		switch v.(type) {
		case *decode.Object:
			nObjs++
		case []any:
			nArrs++
		case string:
			nStrs++
		case json.Number:
			nNums++
		}
	}
	var (
		objs         = make([]*decode.Object, 0, nObjs)
		arrs         = make([][]any, 0, nArrs)
		strs         = make([]string, 0, nStrs)
		nums         = make([]json.Number, 0, nNums)
		bools, nulls int
	)
	for _, v := range vals {
		switch v := v.(type) {
		case *decode.Object:
			objs = append(objs, v)
		case []any:
			arrs = append(arrs, v)
		case string:
			strs = append(strs, v)
		case json.Number:
			nums = append(nums, v)
		case bool:
			bools++
		case nil:
			nulls++
		}
	}
	var branches []*Schema
	if len(objs) > 0 {
		branches = append(branches, in.objects(objs, p)...)
	}
	if len(arrs) > 0 {
		branches = append(branches, in.arrays(arrs, p))
	}
	if len(strs) > 0 {
		branches = append(branches, in.strings(strs, p))
	}
	if len(nums) > 0 {
		branches = append(branches, in.numbers(nums, p))
	}
	if bools > 0 {
		branches = append(branches, &Schema{Type: "boolean"})
	}
	if nulls > 0 {
		branches = append(branches, &Schema{Type: "null"})
	}
	return union(branches)
}

// union combines the schemas of each kind of value seen in one place.
// Plain types are folded into a list of types, and into the one other
// schema if there's only one and its keywords don't constrain them.
func union(bs []*Schema) *Schema {
	if len(bs) == 1 {
		return bs[0]
	}
	var types []string
	var rest []*Schema
	for _, b := range bs {
		if b.typeOnly() {
			types = append(types, b.Type.(string))
		} else {
			rest = append(rest, b)
		}
	}
	if len(types) == 0 {
		return &Schema{AnyOf: rest}
	}
	if len(rest) == 1 {
		if t, ok := rest[0].Type.(string); ok && rest[0].Ref == "" && rest[0].Enum == nil &&
			rest[0].Const == nil && rest[0].AnyOf == nil {
			rest[0].Type = append([]string{t}, types...)
			return rest[0]
		}
	}
	t := &Schema{Type: types}
	if len(types) == 1 {
		t.Type = types[0]
	}
	if len(rest) == 0 {
		return t
	}
	return &Schema{AnyOf: append(rest, t)}
}

func (in *inferrer) strings(vals []string, p place) *Schema {
	counts := map[string]int{}
	var distinct []string // in the order they're first seen
	for _, v := range vals {
		if counts[v] == 0 {
			distinct = append(distinct, v)
		}
		counts[v]++
	}
	if f := commonFormat(distinct); f != "" {
		return &Schema{Type: "string", Format: f}
	}
	if float64(len(distinct)) < in.opts.Cardinality*float64(len(vals)) {
		if !in.enumSafe(distinct, p) {
			return ref(defLowCardinality)
		}
		enum := slices.Clone(distinct)
		sort.Strings(enum)
		return &Schema{Type: "string", Enum: enum}
	}
	// Whether most look random; a sample's plenty to tell.
	sample := distinct[:min(len(distinct), entropySample)]
	high := 0
	for _, v := range sample {
		if highEntropy(v) {
			high++
		}
	}
	if high > 0 && high*2 >= len(sample) {
		return ref(defHighEntropy)
	}
	return &Schema{Type: "string"}
}

// enumSafe reports whether a set of strings can be shown: there aren't
// too many, the key doesn't sound secret, and none of them is long,
// random or opaque, an address or ID, or has a lot of digits.
func (in *inferrer) enumSafe(vals []string, p place) bool {
	if p.sensitive || len(vals) > in.opts.MaxEnum {
		return false
	}
	for _, v := range vals {
		digits := strings.Count(strings.Map(func(r rune) rune {
			if unicode.IsDigit(r) {
				return '0'
			}
			return -1
		}, v), "0")
		if utf8.RuneCountInString(v) > maxEnumLen || strings.ContainsAny(v, "\r\n") ||
			digits >= 6 || format(v) != "" || highEntropy(v) || opaque(v) {
			return false
		}
	}
	return true
}

func (in *inferrer) numbers(vals []json.Number, p place) *Schema {
	s := &Schema{Type: "integer"}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range vals {
		if strings.ContainsAny(string(v), ".eEnN") {
			s.Type = "number" // written as a float, NaN or infinity
		}
		f, err := strconv.ParseFloat(string(v), 64)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			continue
		}
		lo, hi = math.Min(lo, f), math.Max(hi, f)
	}
	if !p.sensitive && lo <= hi {
		s.Minimum, s.Maximum = roundOut(lo, false), roundOut(hi, true)
	}
	return s
}

// roundOut widens a bound away from zero to a power of ten (or zero), so
// a range shows the scale of the numbers without giving any away, and
// fits numbers a little past the ones seen.
func roundOut(x float64, upper bool) json.Number {
	if upper && x <= 0 || !upper && x >= 0 {
		return "0"
	}
	m := math.Abs(x)
	e := 0
	for math.Pow10(e) < m {
		e++
	}
	for e > -300 && math.Pow10(e-1) >= m {
		e--
	}
	v := math.Copysign(math.Pow10(e), x)
	if e >= -6 && e <= 20 {
		return json.Number(strconv.FormatFloat(v, 'f', -1, 64))
	}
	return json.Number(strconv.FormatFloat(v, 'g', -1, 64))
}

func (in *inferrer) arrays(arrs [][]any, p place) *Schema {
	s := &Schema{Type: "array"}
	n, sameLen, total := len(arrs[0]), true, 0
	for _, a := range arrs {
		total += len(a)
		sameLen = sameLen && len(a) == n
	}
	all := make([]any, 0, total)
	for _, a := range arrs {
		all = append(all, a...)
	}
	if len(all) == 0 {
		s.Description = "Only empty arrays were seen."
		return s
	}
	if sameLen && n >= 2 && n <= maxTuple {
		cols := make([][]any, n)
		for i := range cols {
			cols[i] = make([]any, 0, len(arrs))
		}
		for _, a := range arrs {
			for i, v := range a {
				cols[i] = append(cols[i], v)
			}
		}
		mixed := false
		for _, col := range cols[1:] {
			mixed = mixed || kinds(col) != kinds(cols[0])
		}
		// A short array with different things in each position is a
		// tuple; so are several alike ones whose positions differ in any
		// way.
		if len(arrs) > 1 || mixed && n <= maxLoneTuple {
			items := make([]*Schema, n)
			for i, col := range cols {
				items[i] = in.infer(col, p)
			}
			if mixed || !allSame(items, len(arrs) >= minRangedTuple) {
				s.PrefixItems, s.Items, s.MinItems = items, false, &n
				return s
			}
		}
	}
	s.Items = in.infer(all, p)
	if sameLen && len(arrs) >= 3 {
		s.MinItems, s.MaxItems = &n, &n
	}
	return s
}

// kinds names the kinds of value in vals.
func kinds(vals []any) string {
	var k [6]bool
	for _, v := range vals {
		switch v.(type) {
		case *decode.Object:
			k[0] = true
		case []any:
			k[1] = true
		case string:
			k[2] = true
		case json.Number:
			k[3] = true
		case bool:
			k[4] = true
		case nil:
			k[5] = true
		}
	}
	var b strings.Builder
	for i, set := range k {
		if set {
			b.WriteByte("oasnbz"[i])
		}
	}
	return b.String()
}

// allSame reports whether schemas are all alike, counting numeric ranges
// only if ranges is set.
func allSame(ss []*Schema, ranges bool) bool {
	key := func(s *Schema) string {
		if !ranges {
			s = withoutRanges(s)
		}
		b, _ := Marshal(s, "")
		return string(b)
	}
	first := key(ss[0])
	for _, s := range ss[1:] {
		if key(s) != first {
			return false
		}
	}
	return true
}

// withoutRanges returns a copy of s with no minimums or maximums in it.
func withoutRanges(s *Schema) *Schema {
	c := *s
	c.Minimum, c.Maximum = "", ""
	if c.Properties != nil {
		c.Properties = &Properties{}
		for _, k := range s.Properties.keys {
			c.Properties.Set(k, withoutRanges(s.Properties.m[k]))
		}
	}
	if sub, ok := c.AdditionalProperties.(*Schema); ok {
		c.AdditionalProperties = withoutRanges(sub)
	}
	if sub, ok := c.Items.(*Schema); ok {
		c.Items = withoutRanges(sub)
	}
	c.PrefixItems, c.AnyOf = nil, nil
	for _, sub := range s.PrefixItems {
		c.PrefixItems = append(c.PrefixItems, withoutRanges(sub))
	}
	for _, sub := range s.AnyOf {
		c.AnyOf = append(c.AnyOf, withoutRanges(sub))
	}
	return &c
}
