// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Naomi Persephone Amethyst <naomi@amethyst.name>

package decode

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding/htmlindex"
)

// Inner is the key an XML element's content goes under, beside its
// attributes. XML names can't contain "$", so it never clashes with one.
const Inner = "$inner"

// decodeXML reads an XML document into an array of its root elements.
// Each element is an object of one key, its name, holding an object of its
// attributes, with its content under "$inner": the text, if that's all
// there is, or else an array of its child elements, and of any text
// between them. Text and attribute values read as bools, numbers or
// strings; whitespace between elements, comments and processing
// instructions are dropped.
func decodeXML(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Entity = xml.HTMLEntity
	d.CharsetReader = func(label string, r io.Reader) (io.Reader, error) {
		enc, err := htmlindex.Get(label)
		if err != nil {
			return nil, err
		}
		return enc.NewDecoder().Reader(r), nil
	}

	type element struct {
		name     string
		attrs    *Object
		content  []any
		children bool // whether content holds any elements
		text     strings.Builder
	}
	flush := func(e *element) {
		if s := strings.TrimSpace(e.text.String()); s != "" {
			e.content = append(e.content, scalar(s))
		}
		e.text.Reset()
	}
	var stack []*element
	var roots []any
	for {
		tok, err := d.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) >= maxDepth {
				return nil, fmt.Errorf("XML: line %d: nested too deeply", line(d, data))
			}
			if len(stack) > 0 {
				flush(stack[len(stack)-1])
			}
			e := &element{name: xmlName(t.Name), attrs: NewObject()}
			for _, a := range t.Attr {
				e.attrs.Set(xmlName(a.Name), scalar(a.Value))
			}
			stack = append(stack, e)
		case xml.EndElement:
			name := xmlName(t.Name)
			if len(stack) == 0 || stack[len(stack)-1].name != name {
				return nil, fmt.Errorf("XML: line %d: unexpected </%s>", line(d, data), name)
			}
			e := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			flush(e)
			switch {
			case e.children:
				e.attrs.Set(Inner, e.content)
			case len(e.content) == 1:
				e.attrs.Set(Inner, e.content[0])
			}
			wrapped := NewObject()
			wrapped.Set(e.name, e.attrs)
			if len(stack) == 0 {
				roots = append(roots, wrapped)
			} else {
				parent := stack[len(stack)-1]
				parent.content = append(parent.content, wrapped)
				parent.children = true
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			}
		}
	}
	if len(stack) > 0 {
		return nil, fmt.Errorf("XML: <%s> is never closed", stack[len(stack)-1].name)
	}
	if len(roots) == 0 {
		return nil, errors.New("XML: no elements")
	}
	return roots, nil
}

// xmlName writes a name as it was in the document, prefix and all.
func xmlName(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

func line(d *xml.Decoder, data []byte) int {
	return bytes.Count(data[:min(int(d.InputOffset()), len(data))], []byte("\n")) + 1
}
