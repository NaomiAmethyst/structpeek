// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Labels is the key an HCL block's labels go under, beside its body. HCL
// names can't contain "$", so it never clashes with one.
const Labels = "$labels"

// decodeHCL reads HCL, as in Terraform (.tf, .tfvars), Nomad and Packer
// files, into an object of its attributes and blocks. Blocks of a type
// are an array, in the order they come, each an object of its body, with
// its labels (if it has any) under "$labels":
//
//	resource "aws_instance" "web" { ami = "x" }
//
// reads as {"resource": [{"$labels": ["aws_instance", "web"], "ami": "x"}]}.
// Expressions that can be worked out without variables or functions are
// their values; others, like var.region or "${local.name}-x", are kept as
// written, as strings.
func decodeHCL(data []byte) (any, error) {
	f, diags := hclsyntax.ParseConfig(data, "input.hcl", hcl.InitialPos)
	if diags.HasErrors() {
		return nil, fmt.Errorf("HCL: %s", diags.Error())
	}
	return hclBody(f.Body.(*hclsyntax.Body), data), nil
}

func hclBody(b *hclsyntax.Body, src []byte) *Object {
	type item struct {
		pos   int
		attr  *hclsyntax.Attribute
		block *hclsyntax.Block
	}
	var items []item
	for _, a := range b.Attributes {
		items = append(items, item{pos: a.SrcRange.Start.Byte, attr: a})
	}
	for _, bl := range b.Blocks {
		items = append(items, item{pos: bl.TypeRange.Start.Byte, block: bl})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].pos < items[j].pos })

	o := NewObject()
	blockTypes := map[string]bool{}
	for _, it := range items {
		if it.attr != nil {
			o.Set(it.attr.Name, hclExpr(it.attr.Expr, src))
			continue
		}
		body := hclBody(it.block.Body, src)
		if len(it.block.Labels) > 0 {
			labels := make([]any, len(it.block.Labels))
			for i, l := range it.block.Labels {
				labels[i] = l
			}
			withLabels := NewObject()
			withLabels.Set(Labels, labels)
			for i, k := range body.Keys {
				withLabels.Set(k, body.Values[i])
			}
			body = withLabels
		}
		var list []any
		if blockTypes[it.block.Type] {
			prev, _ := o.Get(it.block.Type)
			list = prev.([]any)
		}
		blockTypes[it.block.Type] = true
		o.Set(it.block.Type, append(list, body))
	}
	return o
}

func hclExpr(e hclsyntax.Expression, src []byte) any {
	switch e := e.(type) {
	case *hclsyntax.ObjectConsExpr:
		o := NewObject()
		for _, item := range e.Items {
			var key string
			if k := hcl.ExprAsKeyword(item.KeyExpr); k != "" {
				key = k
			} else if v, diags := item.KeyExpr.Value(nil); !diags.HasErrors() && v.Type() == cty.String && v.IsKnown() && !v.IsNull() {
				key = v.AsString()
			} else {
				key = hclSource(item.KeyExpr, src)
			}
			o.Set(key, hclExpr(item.ValueExpr, src))
		}
		return o
	case *hclsyntax.TupleConsExpr:
		a := make([]any, len(e.Exprs))
		for i, x := range e.Exprs {
			a[i] = hclExpr(x, src)
		}
		return a
	case *hclsyntax.ObjectConsKeyExpr:
		return hclExpr(e.Wrapped, src)
	}
	if v, diags := e.Value(nil); !diags.HasErrors() {
		return ctyValue(v)
	}
	return hclSource(e, src)
}

// hclSource is an expression as written, without the quotes round a
// template.
func hclSource(e hclsyntax.Expression, src []byte) string {
	r := e.Range()
	s := string(src[r.Start.Byte:r.End.Byte])
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

func ctyValue(v cty.Value) any {
	if v.IsNull() || !v.IsKnown() {
		return nil
	}
	v, _ = v.Unmark()
	t := v.Type()
	switch {
	case t == cty.String:
		return v.AsString()
	case t == cty.Number:
		f := v.AsBigFloat()
		if f.IsInt() {
			i, _ := f.Int(nil)
			return json.Number(i.String())
		}
		return json.Number(f.Text('g', -1))
	case t == cty.Bool:
		return v.True()
	case t.IsListType() || t.IsTupleType() || t.IsSetType():
		a := []any{}
		for it := v.ElementIterator(); it.Next(); {
			_, e := it.Element()
			a = append(a, ctyValue(e))
		}
		return a
	case t.IsMapType() || t.IsObjectType():
		o := NewObject()
		for it := v.ElementIterator(); it.Next(); {
			k, e := it.Element()
			o.Set(k.AsString(), ctyValue(e))
		}
		return o
	}
	return v.GoString()
}
