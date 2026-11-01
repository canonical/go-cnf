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

package cnfast

import (
	"fmt"
	"strings"
)

// RenderOptions are the settings of one Render call. None exist yet; the
// parameter exists so settings can be added without a breaking change.
type RenderOptions struct{}

// Render renders the provided hierarchy as cnf text.
func Render(root *Item, opts *RenderOptions) ([]byte, error) {
	var b strings.Builder
	// The root value is a keyless item; its comment runs render around it.
	writeCmt(&b, root.PreCmt, "")
	if isBlock(root.Value) {
		// The root has no pair to open under: its block sits at column zero.
		if err := writeBlock(&b, root.Value, "", root.HeadCmt, root.FootCmt); err != nil {
			return nil, err
		}
	} else {
		if err := writeValue(&b, root.Value); err != nil {
			return nil, err
		}
		if len(root.InCmt.Text) > 0 {
			b.WriteString(root.InCmt.Text[0])
		}
		b.WriteString("\n")
	}
	writeCmt(&b, root.PostCmt, "")
	return []byte(b.String()), nil
}

// writeCmt writes one comment run at the given indent, a newline after
// each line; a blank line is the empty string, so it gets no indent.
func writeCmt(b *strings.Builder, c Cmt, indent string) {
	for _, ln := range c.Text {
		if ln != "" {
			b.WriteString(indent)
			b.WriteString(ln)
		}
		b.WriteString("\n")
	}
}

func isBlock(v Value) bool {
	switch n := v.(type) {
	case *Map:
		return n.Block
	case *List:
		return n.Block
	case *Str:
		return n.Block
	}
	return false
}

// writeBlock writes one block value at the given indent, its own fields
// carrying the form: a map renders its pairs, a list its items or ---
// delimiters, a string its fence.
func writeBlock(b *strings.Builder, v Value, indent string, head, foot Cmt) error {
	switch n := v.(type) {
	case *Map:
		return writeMap(b, n, indent, head, foot)
	case *List:
		return writeBlockList(b, n, indent, head, foot)
	case *Str:
		return writeStr(b, n, indent)
	}

	return fmt.Errorf("cannot render %T as a block", v)
}

// writeStr writes one string in the asked layout: bare, quoted, compact
// fence inline, or the fence block. Text nil spells the content fresh;
// Text set renders verbatim, and when Str also holds a value the two must
// agree, else the render refuses rather than pick a side. A record writes
// verbatim only in the layout it was made for.
func writeStr(b *strings.Builder, s *Str, indent string) error {
	// A recorded Text must decode to the content - it is what a respell
	// writes - and when Str also holds one, the two must agree.
	texted, decoded := false, s.Data
	if s.Text != nil {
		var db strings.Builder
		if err := s.decode(&db); err != nil {
			return err
		}
		v := db.String()
		if len(s.Data) > 0 && v != s.Data {
			return fmt.Errorf("cannot render stale string spelling")
		}
		texted, decoded = true, v
	}
	fenced := s.Quote == SingleFenced || s.Quote == DoubleFenced
	// A compact record: one element carrying no tail - the recorded
	// interior of a one-line fence.
	compact := texted && fenced && len(s.Text) == 1 &&
		!strings.HasSuffix(s.Text[0], "\n") &&
		!oddTrailingBackslashes(s.Text[0])
	if !s.Block {
		if compact {
			// The recorded interior writes verbatim between
			// the delimiters.
			delim := "'''"
			if s.Quote == DoubleFenced {
				delim = `"""`
			}
			b.WriteString(delim)
			b.WriteString(s.Text[0])
			b.WriteString(delim)
			return nil
		}
		quoteString(b, decoded, s.Quote, true)
		return nil
	}
	if !fenced {
		// A quoted family demanded as a block: no such shape.
		return fmt.Errorf("cannot render %T as a block", s)
	}
	// A recorded block - made for this shape, every element carrying its
	// own tail - writes verbatim.
	if texted && !compact {
		delim := "'''"
		if s.Quote == DoubleFenced {
			delim = `"""`
		}
		b.WriteString(indent)
		b.WriteString(delim)
		b.WriteString("\n")
		for _, el := range s.Text {
			b.WriteString(indent)
			b.WriteString(el)
			if !strings.HasSuffix(el, "\n") {
				b.WriteString("\n")
			}
		}
		b.WriteString(indent)
		b.WriteString(delim)
		b.WriteString("\n")
		return nil
	}
	// Everything else spells fresh: encode writes the fence from the
	// content, marking away a missing final newline.
	_, err := s.encode(b, indent, nil)
	return err
}

// decode writes the decoded value of Text into b, without storing it into
// Str. Each element follows the fence rules: a marker tail continues onto
// the next element, and every other tail is part of the value.
func (s *Str) decode(b *strings.Builder) error {
	if s.Text == nil {
		b.WriteString(s.Data)
		return nil
	}
	for _, line := range s.Text {
		// Every element ends in a tail: a newline is part of the value;
		// a backslash stands for an eaten newline, continuing onto the
		// next element.
		continued := false
		if s.Quote == DoubleFenced && oddTrailingBackslashes(line) {
			line = line[:len(line)-1]
			continued = true
		} else if strings.HasSuffix(line, "\n") {
			line = strings.TrimSuffix(line, "\n")
		} else {
			// Neither tail: the lone element of a recorded one-line
			// fence, the whole value with no terminator.
			continued = true
		}
		if s.Quote == DoubleFenced {
			unq, err := unescape(line)
			if err != nil {
				return err
			}
			line = unq
		}
		b.WriteString(line)
		if !continued {
			b.WriteString("\n")
		}
	}
	return nil
}

// encode writes s as a block fence at indent, spelling its content fresh
// for the family resolveQuote picks. When textf is set, it is called with
// each encoded line - the content and its tail, indent stripped.
func (s *Str) encode(b *strings.Builder, indent string, textf func(string)) (QuoteKind, error) {
	query := resolveQuote(s.Data, s.Quote, true)
	delim := "'''"
	if query == DoubleFenced {
		delim = `"""`
	}
	value := s.Data
	endsNewline := strings.HasSuffix(value, "\n")
	lines := strings.Split(value, "\n")
	if endsNewline {
		lines = lines[:len(lines)-1] // the final empty element is the terminator, not a line
	}
	if len(value) == 0 {
		lines = nil // an empty value writes no content lines
	}
	b.WriteString(indent)
	b.WriteString(delim)
	b.WriteString("\n")
	for i, line := range lines {
		b.WriteString(indent)
		start := b.Len() // the element begins past the indent
		switch {
		case query != DoubleFenced:
			b.WriteString(line)
		case line == `"""`:
			// A content line equal to the delimiter would close the fence
			// on re-parse; one escaped quote keeps it open and decodes
			// back to the same value.
			b.WriteString(`\"""`)
		default:
			escapeDoubleFenced(b, line)
		}
		// The element's tail: a marker when the value ends without a
		// newline - only the escaped family can spell it - else the
		// terminator newline.
		marker := query == DoubleFenced && i == len(lines)-1 && !endsNewline
		if marker {
			b.WriteString(`\`)
		} else {
			b.WriteString("\n")
		}
		if textf != nil {
			textf(b.String()[start:b.Len()])
		}
		// A marker line still needs a physical newline to close it;
		// the terminator's newline already is one.
		if marker {
			b.WriteString("\n")
		}
	}
	b.WriteString(indent)
	b.WriteString(delim)
	b.WriteString("\n")
	return query, nil
}

// writeBlockList writes one block list's items at the given indent. A
// Dashed list writes each item as a "- " line when every item sits on one
// line; anything else renders each element between --- delimiters.
func writeBlockList(b *strings.Builder, l *List, indent string, head, foot Cmt) error {
	// The dashed form asks every item to sit on one line; a block item
	// cannot, and the render falls back to the --- delimited form, which
	// holds any element.
	dashed := l.Dashed
	if dashed {
		for _, item := range l.Items {
			if isBlock(item.Value) {
				dashed = false
				break
			}
		}
	}
	if !dashed {
		b.WriteString(indent)
		b.WriteString("---\n")
		for _, item := range l.Items {
			if len(item.Key) > 0 {
				// A keyed element in the fence form renders as its own one-pair
				// map: the dash spelling has no fence counterpart.
				if err := writeMap(b, &Map{Items: []*Item{item}, Block: true}, indent, Cmt{}, Cmt{}); err != nil {
					return err
				}
			} else if !isBlock(item.Value) {
				// A scalar element: the value on the element's only line.
				b.WriteString(indent)
				if err := writeValue(b, item.Value); err != nil {
					return err
				}
				if len(item.InCmt.Text) > 0 {
					b.WriteString(item.InCmt.Text[0])
				}
				b.WriteString("\n")
			} else if nested, ok := item.Value.(*List); ok && !nested.Dashed {
				// A nested list fence opens one indent under the fence;
				// dashed lists render at the fence's indent.
				if err := writeBlock(b, item.Value, indent+item.Indent, item.HeadCmt, item.FootCmt); err != nil {
					return err
				}
			} else if err := writeBlock(b, item.Value, indent, item.HeadCmt, item.FootCmt); err != nil {
				return err
			}
			b.WriteString(indent)
			b.WriteString("---\n")
		}
		return nil
	}
	writeCmt(b, head, indent)
	above := len(head.Text) > 0
	for _, item := range l.Items {
		if above {
			b.WriteString("\n") // the implicit divider
		}
		writeCmt(b, item.PreCmt, indent)
		b.WriteString(indent)
		b.WriteString("- ")
		if len(item.Key) > 0 {
			if err := writeItemKey(b, item); err != nil {
				return err
			}
			b.WriteString(": ")
		}
		if err := writeValue(b, item.Value); err != nil {
			return err
		}
		if len(item.InCmt.Text) > 0 {
			b.WriteString(item.InCmt.Text[0])
		}
		b.WriteString("\n")
		writeCmt(b, item.PostCmt, indent)
		above = len(item.PostCmt.Text) > 0
	}
	if above && len(foot.Text) > 0 {
		b.WriteString("\n") // the implicit divider
	}
	writeCmt(b, foot, indent)
	return nil
}

// writeMap writes one map at the given indent, inline on one line or in
// block form with the comment runs around the items. The blank divider
// between two non-empty runs is implicit - dropped by the parse, and
// re-injected here, one blank after each run a comment follows.
func writeMap(b *strings.Builder, m *Map, indent string, head, foot Cmt) error {
	if !m.Block {
		b.WriteString("{")
		for i, item := range m.Items {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := writeItemKey(b, item); err != nil {
				return err
			}
			b.WriteString(": ")
			if err := writeValue(b, item.Value); err != nil {
				return err
			}
		}
		b.WriteString("}")
		return nil
	}
	writeCmt(b, head, indent)
	above := len(head.Text) > 0
	for _, item := range m.Items {
		if above {
			b.WriteString("\n") // the implicit divider
		}
		writeCmt(b, item.PreCmt, indent)
		b.WriteString(indent)
		if err := writeItemKey(b, item); err != nil {
			return err
		}
		b.WriteString(":")

		// A value that cannot spell its recorded form in this position takes the
		// spelling the grammar allows: an empty block container respells inline
		// - it has no block form - and a fresh unquoted string with an inner
		// newline opens as a fence block. The parse never records Unquoted in
		// value position, so this serves trees built outside the parse.
		value := item.Value
		switch n := value.(type) {
		case *Map:
			if len(n.Items) == 0 && n.Block {
				shallow := *n
				shallow.Block = false
				value = &shallow
			}
		case *List:
			if len(n.Items) == 0 && n.Block && n.Dashed {
				shallow := *n
				shallow.Block = false
				value = &shallow
			}
		case *Str:
			if n.Quote == Unquoted && strings.Contains(strings.TrimRight(n.Data, "\n"), "\n") {
				shallow := *n
				shallow.Quote = resolveQuote(n.Data, SingleFenced, true)
				shallow.Block = true
				value = &shallow
			}
		}

		// The value is written in its recorded form: inline on the pair's
		// line, or a block opening at the item's Indent - which holds any
		// value, even one without newlines, through the backslash marker.
		if !isBlock(value) {
			b.WriteString(" ")
			if err := writeValue(b, value); err != nil {
				return err
			}
			if len(item.InCmt.Text) > 0 {
				b.WriteString(item.InCmt.Text[0])
			}
			b.WriteString("\n")
		} else {
			b.WriteString("\n")
			// The block sits at the item's own indent; a synthetic tree
			// records none, so the default step - four spaces - is written.
			step := item.Indent
			if step == "" {
				step = "    "
			}
			if err := writeBlock(b, value, indent+step, item.HeadCmt, item.FootCmt); err != nil {
				return err
			}
		}
		writeCmt(b, item.PostCmt, indent)
		above = len(item.PostCmt.Text) > 0
	}
	if above && len(foot.Text) > 0 {
		b.WriteString("\n") // the implicit divider
	}
	writeCmt(b, foot, indent)
	return nil
}

// writeItemKey writes one item's dotted key path.
func writeItemKey(b *strings.Builder, item *Item) error {
	for i, k := range item.Key {
		if i > 0 {
			b.WriteString(".")
		}
		quoteString(b, k.Data, k.Quote, false)
	}
	return nil
}

// writeValue writes one value node in its inline form.
func writeValue(b *strings.Builder, v any) error {
	switch n := v.(type) {
	case *Str:
		// An inline context asks the one-line spellings: a fence-block
		// record respells to its compact form.
		if n.Block {
			shallow := *n
			shallow.Block = false
			n = &shallow
		}
		return writeStr(b, n, "")
	case *Num:
		b.WriteString(n.Text)
	case *Scalar:
		b.WriteString(n.Text)
	case *Map:
		// An item claiming a block form inside an inline map is a malformed
		// AST, rejected rather than silently normalized. A list holds the
		// same check.
		for _, item := range n.Items {
			if isBlock(item.Value) {
				return fmt.Errorf("cannot render a block value inside an inline map")
			}
		}
		return writeMap(b, n, "", Cmt{}, Cmt{})
	case *List:
		for _, item := range n.Items {
			if isBlock(item.Value) {
				return fmt.Errorf("cannot render a block value inside an inline list")
			}
		}
		b.WriteString("[")
		for i, item := range n.Items {
			if i > 0 {
				b.WriteString(", ")
			}
			if err := writeValue(b, item.Value); err != nil {
				return err
			}
		}
		b.WriteString("]")
	default:
		return fmt.Errorf("cannot render %T", v)
	}
	return nil
}

// escapeRune spells one rune inside a double-quoted string: the
// closed escape set, \xNN canonical for the C0 controls.
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
		return fmt.Sprintf(`\x%02x`, r)
	case r == 0x2028 || r == 0x2029:
		return fmt.Sprintf(`\u%04x`, r)
	}
	return string(r)
}

// isBareWord reports whether s can spell as one: non-empty,
// every rune in the bare set.
func isBareWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isBareRune(r) {
			return false
		}
	}
	return true
}

// escapeDoubleFenced spells s inside a fence family: the closed escape
// set over the value, the quote kept raw - a lone quote needs no escape.
func escapeDoubleFenced(b *strings.Builder, s string) {
	for _, r := range s {
		if r == '"' {
			b.WriteRune(r)
			continue
		}
		b.WriteString(escapeRune(r))
	}
}

// escapeDouble spells s inside a double-quoted string: the
// closed escape set, the quote and the backslash escaped.
func escapeDouble(b *strings.Builder, s string) {
	for _, r := range s {
		b.WriteString(escapeRune(r))
	}
}

// resolveQuote picks the quote family that spells s in the asked shape,
// inline or block, escalating an ask that cannot hold s to the cheapest
// family that can. The quoted families are inline-only, the compact raw
// fence holds no newline, and the block raw fence needs a final one.
func resolveQuote(s string, q QuoteKind, block bool) QuoteKind {
	switch q {
	case Unquoted:
		if isBareWord(s) {
			break
		}
		q = DoubleQuoted
		if strings.ContainsRune(s, '"') || strings.ContainsRune(s, '\\') {
			// The single family spells both raw, the cleaner spelling -
			// when it holds the rest of the content.
			q = SingleQuoted
			if strings.ContainsRune(s, '\'') || strings.ContainsRune(s, '\n') {
				q = DoubleQuoted
			}
		}
	case SingleQuoted:
		if strings.ContainsRune(s, '\'') || strings.ContainsRune(s, '\n') {
			q = DoubleQuoted
		}
	case DoubleQuoted:
	case SingleFenced:
		if !block {
			// The compact raw family holds no newline, no interior
			// delimiter, and no final quote - the close would swallow
			// it into a four-quote typo.
			if strings.Contains(s, "'''") || strings.ContainsRune(s, '\n') ||
				strings.HasSuffix(s, "'") {
				q = DoubleFenced
			}
			break
		}
		// The block raw family holds newlines but cannot mark a missing
		// final one, and a content line equal to the delimiter would
		// close the fence early.
		if !strings.HasSuffix(s, "\n") {
			q = DoubleFenced
			break
		}
		for _, line := range strings.Split(s, "\n") {
			if line == "'''" {
				q = DoubleFenced
				break
			}
		}
	case DoubleFenced:
	}
	return q
}

// quoteString writes one string in the asked family, never failing:
// resolveQuote escalates any ask that cannot hold it. force is value
// position: a bare word is never read back, so its spelling is skipped.
func quoteString(b *strings.Builder, s string, q QuoteKind, force bool) {
	q = resolveQuote(s, q, false)
	if force && q == Unquoted {
		// Value position: the ladder kept the word bare, a spelling the
		// grammar never reads back there.
		q = DoubleQuoted
		if strings.ContainsRune(s, '"') || strings.ContainsRune(s, '\\') {
			q = SingleQuoted
			if strings.ContainsRune(s, '\'') || strings.ContainsRune(s, '\n') {
				q = DoubleQuoted
			}
		}
	}
	switch q {
	case Unquoted:
		b.WriteString(s)
	case SingleQuoted:
		b.WriteString("'")
		b.WriteString(s)
		b.WriteString("'")
	case DoubleQuoted:
		b.WriteRune('"')
		escapeDouble(b, s)
		b.WriteRune('"')
	case SingleFenced:
		b.WriteString("'''")
		b.WriteString(s)
		b.WriteString("'''")
	case DoubleFenced:
		// A final raw quote would run into the closing delimiter, so it
		// is escaped - the off-band tail of the compact close.
		body, tail := s, ""
		if strings.HasSuffix(s, `"`) {
			body, tail = s[:len(s)-1], `\"`
		}
		b.WriteString(`"""`)
		escapeDoubleFenced(b, body)
		b.WriteString(tail)
		b.WriteString(`"""`)
	}
}

// Quote returns s quoted and escaped in the q family.
func Quote(s string, q QuoteKind) string {
	var b strings.Builder
	quoteString(&b, s, q, false)
	return b.String()
}
