// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/calico32/kdl-go"
)

// The keys a KDL node's arguments and children go under, beside its
// properties.
const (
	Args     = "$args"
	Children = "$children"
)

// decodeKDL reads a KDL document, version 1 or 2, into an array of its
// nodes. Each node is an object of one key, its name, holding an object of
// its properties, with its arguments, if it has any, in an array under
// "$args", and its child nodes, if it has any, in one under "$children":
//
//	node 1 key="v" { child #true }
//
// reads as [{"node": {"$args": [1], "key": "v", "$children": [{"child":
// {"$args": [true]}}]}}]. Type annotations are dropped, and a property
// given twice keeps its last value, as the spec says.
func decodeKDL(data []byte) (any, error) {
	doc, err := parseKDL(data)
	if err != nil {
		return nil, err
	}
	return kdlNodes(doc), nil
}

func parseKDL(data []byte) (*kdl.Document, error) {
	if err := checkNesting("KDL", data, "{", "}", false); err != nil {
		return nil, err
	}
	doc, err := kdl.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("KDL: %w", redact(err, data))
	}
	return doc, nil
}

// kdlStructured reports whether a document has any properties or children.
// Plain text is often valid KDL, as "just some text" is a node with two
// arguments, so only a document with more to it can be recognised as KDL.
func kdlStructured(doc *kdl.Document) bool {
	for _, n := range doc.Nodes {
		if len(n.PropertyOrder()) > 0 || len(n.Children().Nodes) > 0 {
			return true
		}
	}
	return false
}

func kdlNodes(doc *kdl.Document) []any {
	nodes := make([]any, len(doc.Nodes))
	for i, n := range doc.Nodes {
		body := NewObject()
		if args := n.Arguments(); len(args) > 0 {
			a := make([]any, len(args))
			for j, v := range args {
				a[j] = kdlValue(v)
			}
			body.Set(Args, a)
		}
		for _, k := range n.PropertyOrder() {
			key := k
			// "$" can start a KDL name, so a property could clash.
			for key == Args || key == Children {
				key = "$" + key
			}
			body.Set(key, kdlValue(n.Prop(k)))
		}
		if len(n.Children().Nodes) > 0 {
			body.Set(Children, kdlNodes(n.Children()))
		}
		wrapped := NewObject()
		wrapped.Set(n.Name(), body)
		nodes[i] = wrapped
	}
	return nodes
}

func kdlValue(v kdl.Value) any {
	switch v.Kind() {
	case kdl.String:
		return v.String()
	case kdl.Int:
		return json.Number(strconv.FormatInt(int64(v.Int()), 10))
	case kdl.Float:
		return floatNumber(v.Float())
	case kdl.BigInt:
		return json.Number(v.BigInt().String())
	case kdl.BigFloat:
		return json.Number(v.BigFloat().Text('g', -1))
	case kdl.Bool:
		return v.Bool()
	}
	return nil
}
