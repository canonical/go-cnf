// Copyright 2026 Canonical Inc.
// Originally by Gustavo Niemeyer.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package cnfjson renders cnf ASTs as JSON text.
//
// The rendering is a walk over the AST: keys keep their
// document order, strings re-escape into JSON's double-quoted
// form, and numbers normalize - the decimal forms pass
// through verbatim, keeping their zeros and their precision,
// and the other bases convert to decimal, any width, by
// repeated division of the digit string.
package cnfjson

import (
	"fmt"
	"strings"

	"github.com/canonical/go-cnf/cnfast"
	"github.com/canonical/go-cnf/cnfpretty"
)

// RenderJSON renders one AST - a *cnfast.Item, the root - as
// the JSON text it represents: one JSON value, structurally
// identical to the cnf. The root is usually a map, but may be
// any value. The paths unfold first - every dotted key splits
// into nesting levels, the levels merging by key - because
// JSON has no dotted keys: the unfolded tree is the JSON
// shape.
func RenderJSON(ast any) ([]byte, error) {
	root, ok := ast.(*cnfast.Item)
	if !ok {
		return nil, fmt.Errorf("cannot render %T", ast)
	}
	// The unfold mutates the tree it is given, so the render
	// clones first: the caller's AST is never touched.
	root = cnfpretty.Clone(root)
	unfold := cnfpretty.Options{UnfoldMapFromItems: 1, UnfoldPathFromDepth: 1}
	if err := cnfpretty.Apply(root, &unfold); err != nil {
		return nil, err
	}
	var b strings.Builder
	if err := writeValue(&b, root.Value); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// ToJSON converts one cnf document to the JSON text it
// represents: the document parses into the AST, and the AST
// renders.
func ToJSON(data []byte) ([]byte, error) {
	ast, err := cnfast.Parse(data, nil)
	if err != nil {
		return nil, err
	}
	return RenderJSON(ast)
}

// writeValue writes one value node as JSON.
func writeValue(b *strings.Builder, v any) error {
	switch n := v.(type) {
	case *cnfast.Str:
		return writeString(b, n)
	case *cnfast.Num:
		return writeNumber(b, n)
	case *cnfast.Scalar:
		b.WriteString(n.Text)
		return nil
	case *cnfast.Map:
		b.WriteString("{")
		for i, item := range n.Items {
			if i > 0 {
				b.WriteString(",")
			}
			if err := writeKey(b, item.Key); err != nil {
				return err
			}
			b.WriteString(":")
			if err := writeValue(b, item.Value); err != nil {
				return err
			}
		}
		b.WriteString("}")
		return nil
	case *cnfast.List:
		b.WriteString("[")
		for i, item := range n.Items {
			if i > 0 {
				b.WriteString(",")
			}
			v := item.Value
			if len(item.Key) > 0 {
				// A keyed element ("- label: x" in cnf) renders
				// as the object the element's pair spells, the
				// key wrapping the value the way a map key does.
				v = keyWrap(item.Key, v)
			}
			if err := writeValue(b, v); err != nil {
				return err
			}
		}
		b.WriteString("]")
		return nil
	}
	return fmt.Errorf("cannot render %T", v)
}

// keyWrap builds one element's keyed value as JSON sees it:
// each key segment one single-pair map level deep, the value
// innermost - a one-segment key wraps the value directly, a
// multi-segment key nests one object per segment. Each map
// holds a single-segment key, so writeKey's one-segment rule
// holds for the wraps as for the rest of the unfolded tree.
func keyWrap(key []*cnfast.Str, v cnfast.Value) cnfast.Value {
	for i := len(key) - 1; i >= 0; i-- {
		v = &cnfast.Map{Items: []*cnfast.Item{{Key: key[i : i+1], Value: v}}}
	}
	return v
}

// writeKey writes one item's key as a JSON string: the
// unfolded tree holds single-segment keys, so the one segment
// is written quoted, the decoded text escaped into JSON's
// double-quoted form.
func writeKey(b *strings.Builder, key []*cnfast.Str) error {
	if len(key) != 1 {
		return fmt.Errorf("cannot render key of %d segments", len(key))
	}
	return writeQuoted(b, key[0].Data)
}

// writeString writes one string value as JSON: the decoded
// text escaped into the double-quoted form.
func writeString(b *strings.Builder, s *cnfast.Str) error {
	return writeQuoted(b, s.Data)
}

// writeQuoted writes s as a JSON string literal.
func writeQuoted(b *strings.Builder, s string) error {
	b.WriteString(`"`)
	for _, r := range s {
		b.WriteString(escapeRune(r))
	}
	b.WriteString(`"`)
	return nil
}

// escapeRune spells one rune inside a JSON double-quoted
// string: the closed set escaped, the rest verbatim.
func escapeRune(r rune) string {
	switch {
	case r == '"':
		return `\"`
	case r == '\\':
		return `\\`
	case r == '\n':
		return `\n`
	case r == '\r':
		return `\r`
	case r == '\t':
		return `\t`
	case r == '\b':
		return `\b`
	case r == '\f':
		return `\f`
	case r < 0x20:
		return fmt.Sprintf(`\u%04x`, r)
	}
	return string(r)
}
