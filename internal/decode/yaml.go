// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"gopkg.in/yaml.v3"
)

// maxYAMLNodes caps how many nodes a YAML file may expand to once aliases
// are followed, so a "billion laughs" file fails instead of eating memory.
const maxYAMLNodes = 10_000_000

// decodeYAML reads a YAML stream. One document is its value; several
// become an array.
func decodeYAML(data []byte) (any, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	y := &yamlReader{}
	var docs []any
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("YAML: %w", err)
		}
		v, err := y.value(&n, 0)
		if err != nil {
			return nil, err
		}
		docs = append(docs, v)
	}
	switch len(docs) {
	case 0:
		return nil, errors.New("YAML: no documents")
	case 1:
		return docs[0], nil
	}
	return docs, nil
}

type yamlReader struct {
	nodes int
}

func (y *yamlReader) value(n *yaml.Node, depth int) (any, error) {
	if y.nodes++; y.nodes > maxYAMLNodes {
		return nil, errors.New("YAML: too many nodes once aliases are expanded")
	}
	if depth > 10_000 {
		return nil, errors.New("YAML: nested too deeply")
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return y.value(n.Content[0], depth+1)
	case yaml.AliasNode:
		return y.value(n.Alias, depth+1)
	case yaml.SequenceNode:
		a := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := y.value(c, depth+1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		return a, nil
	case yaml.MappingNode:
		o := NewObject()
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Kind == yaml.ScalarNode && k.ShortTag() == "!!merge" {
				if err := y.merge(o, v, depth+1); err != nil {
					return nil, err
				}
				continue
			}
			key, err := y.key(k, depth+1)
			if err != nil {
				return nil, err
			}
			val, err := y.value(v, depth+1)
			if err != nil {
				return nil, err
			}
			o.Set(key, val)
		}
		return o, nil
	case yaml.ScalarNode:
		return yamlScalar(n), nil
	}
	return nil, fmt.Errorf("YAML: line %d: unexpected node", n.Line)
}

// merge handles a "<<" merge key: keys from the merged mappings fill in
// whatever the mapping doesn't set itself.
func (y *yamlReader) merge(o *Object, n *yaml.Node, depth int) error {
	v, err := y.value(n, depth)
	if err != nil {
		return err
	}
	sources := []any{v}
	if a, ok := v.([]any); ok {
		sources = a
	}
	for _, s := range sources {
		m, ok := s.(*Object)
		if !ok {
			return fmt.Errorf("YAML: line %d: can only merge mappings", n.Line)
		}
		for i, k := range m.Keys {
			if _, set := o.Get(k); !set {
				o.Set(k, m.Values[i])
			}
		}
	}
	return nil
}

// key turns a mapping key into a string. Keys that aren't scalars are
// written out as JSON.
func (y *yamlReader) key(n *yaml.Node, depth int) (string, error) {
	for n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value, nil
	}
	v, err := y.value(n, depth)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(Plain(v))
	return string(b), err
}

func yamlScalar(n *yaml.Node) any {
	switch n.ShortTag() {
	case "!!null":
		return nil
	case "!!bool":
		var b bool
		if n.Decode(&b) == nil {
			return b
		}
	case "!!int":
		var i int64
		if n.Decode(&i) == nil {
			return json.Number(strconv.FormatInt(i, 10))
		}
		var u uint64
		if n.Decode(&u) == nil {
			return json.Number(strconv.FormatUint(u, 10))
		}
		if jsonNumber.MatchString(n.Value) {
			return json.Number(n.Value) // too big for 64 bits
		}
	case "!!float":
		var f float64
		if n.Decode(&f) == nil {
			return floatNumber(f)
		}
	}
	return n.Value
}
