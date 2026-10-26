package cnfast

import (
	"fmt"
	"strings"
)

// RenderOptions are the settings of one Render call. None
// exist yet; the parameter exists so that render-side knobs
// can be added later without another breaking change. The
// nil value is the default render.
type RenderOptions struct{}

// Render writes an AST back as cnf text, byte for byte as
// the parse recorded it: every layout and every spelling
// stays as it was read. The AST stores the spellings that
// matter - quote styles, number spellings - so the round
// trip cnf → AST → cnf reproduces the original text for
// canonical spellings. A nested map value renders as a
// block, indented one level deeper than its item. A caller
// who wants the tree re-spelled applies cnfpretty first:
// the reformat owns every re-spelling, the render none.
func Render(ast any, opts *RenderOptions) ([]byte, error) {
	var b strings.Builder
	root, ok := ast.(*Item)
	if !ok {
		return nil, fmt.Errorf("cannot render %T", ast)
	}
	// The root value is a keyless item. The comment runs
	// render around the value: the inline forms on the item's
	// line with their trailing comment, the block forms
	// opening at the value's own Indent, which is empty at
	// the top.
	writeCmt(&b, root.PreCmt, "")
	if isBlock(root.Value) {
		// The root has no pair to open under, so its block
		// sits at column zero.
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

// writeCmt writes one comment run at the given indent: each
// line with the indent re-added, and a newline after each.
// A blank line is the empty string, so it gets no indent.
func writeCmt(b *strings.Builder, c Cmt, indent string) {
	for _, ln := range c.Text {
		if ln != "" {
			b.WriteString(indent)
			b.WriteString(ln)
		}
		b.WriteString("\n")
	}
}

// isBlock reports whether a value is recorded in one of its
// block forms: a block map, a block list, or a fence-block
// string. Anything else - a number, a scalar, an inline
// spelling - sits on its line.
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

// writeBlock writes one block value at the given indent: a
// map, a list, or a fence, chosen by the node's type. The
// node's own fields carry the form: a block map renders its
// pairs, a block list its items or its --- delimiters, a
// string its fence.
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

// writeStr writes one string in the asked layout, every shape
// the grammar holds: bare, quoted, compact fence inline, and
// the fence block at the indent. The four rules of the Text
// contract open the body and decode into one pair - whether
// the recorded Text is written, and the content under either
// source:
//
//	Text nil, Str ""    - fresh, the empty string
//	Text nil, Str set   - fresh: the content spells
//	Text set, Str ""    - recorded: the Text renders unverified
//	Text set, Str set   - recorded when the decode matches Str,
//	                      else the render refuses rather than
//	                      pick a side
//
// The shapes below write the recorded elements verbatim when
// the layout is the one the record was made for, and spell the
// content fresh when it is not.
func writeStr(b *strings.Builder, s *Str, indent string) error {
	// The four rules. A recorded Text always decodes to the
	// content - it is what a respell writes - and when Str
	// also holds one, the two must agree.
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
	// A compact record: one element carrying no tail, the
	// recorded interior of a one-line fence.
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
	// The block fence. A recorded block - made for this shape,
	// every element carrying its own tail - writes verbatim:
	// the opening delimiter, each element with a physical
	// newline only when it lacks one (a marker line), and the
	// closing delimiter.
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
	// Everything else spells fresh: encode writes the fence
	// from the content, choosing the family and marking away a
	// missing final newline. The four rules leave decoded equal
	// to Str here, so encode reads the same content.
	_, err := s.encode(b, indent, nil)
	return err
}

// Decode decodes the string's value. If Text is set, it is
// decoded and stored back into Str; if not, Str is left as is.
// Either way the caller reads the decoded value from Str after
// this call, without checking which of the two fields held it.
//
// Decoding follows the same rules as parsing a fence: each
// element is unescaped for the triple-quoted family, a
// marker tail - an odd run of backslashes - continues onto
// the next element (the marker is dropped, and no newline is
// inserted), and every other tail - the terminator - is kept
// as part of the value. The recorded elements carry their own
// tails, so the decode copies them plainly.
func (s *Str) Decode() error {
	if s.Text == nil {
		return nil
	}
	var b strings.Builder
	if err := s.decode(&b); err != nil {
		return err
	}
	s.Data = b.String()
	return nil
}

// decode writes the decoded value of Text into b without
// storing it into Str. The render uses it to check that Text
// still matches Str before writing either one: if they
// disagree, someone changed Str without calling Set, and the
// render fails rather than guess which of the two is current.
func (s *Str) decode(b *strings.Builder) error {
	if s.Text == nil {
		b.WriteString(s.Data)
		return nil
	}
	for _, line := range s.Text {
		// Every element ends in a tail: a newline, or a
		// backslash. A newline is part of the value.
		// A backslash stands for an eaten newline, so this
		// line is part of the next.
		continued := false
		if s.Quote == DoubleFenced && oddTrailingBackslashes(line) {
			line = line[:len(line)-1]
			continued = true
		} else if strings.HasSuffix(line, "\n") {
			line = strings.TrimSuffix(line, "\n")
		} else {
			// Neither tail: this is a recorded one-line
			// fence, whose lone element is the whole value
			// with no terminator. The value ends here, short
			// of a newline. A freshly spelled line always
			// carries a tail, so only a record reaches here.
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

// Encode breaks s into Text elements for the block fence,
// the mirror of Decode: where Decode reads recorded elements
// back into Str, Encode spells Str out into elements and
// stores them into Text, recording the family it spelled them
// in. It drives encode with a callback that collects each
// encoded line, so the elements are exactly what the fence
// writer emits - no separate spelling to keep in step. The
// caller reads the elements from Text after this call; they
// carry their own tails and no indent.
func (s *Str) Encode() error {
	var throwaway strings.Builder
	var text []string
	query, err := s.encode(&throwaway, "", func(line string) {
		text = append(text, line)
	})
	if err != nil {
		return err
	}
	s.Text, s.Quote = text, query
	return nil
}

// encode writes s as a block fence at indent, spelling its
// content fresh for the family resolveQuote picks. The caller
// has checked that the block form is asked; the inline spelling
// and the recorded-verbatim path stay with writeStr. When textf
// is set, it is called with every encoded line as it is written
// - the content and its own tail, indent stripped, taken from
// the builder between the prefix and the physical newline that
// closes a marker line. It returns the family resolveQuote
// spelled the fence in.
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
			// A content line equal to the delimiter would
			// close the fence early on re-parse; one quote
			// escaped keeps it open and decodes back to the
			// same value.
			b.WriteString(`\"""`)
		default:
			escapeDoubleFenced(b, line)
		}
		// The element's tail: a marker when the value ends
		// without a newline - only the escaped family can
		// spell it - else the terminator newline.
		marker := query == DoubleFenced && i == len(lines)-1 && !endsNewline
		if marker {
			b.WriteString(`\`)
		} else {
			b.WriteString("\n")
		}
		if textf != nil {
			textf(b.String()[start:b.Len()])
		}
		// A marker line still needs a physical newline to
		// close it; the terminator's newline already is one.
		if marker {
			b.WriteString("\n")
		}
	}
	b.WriteString(indent)
	b.WriteString(delim)
	b.WriteString("\n")
	return query, nil
}

// writeBlockList writes one block list's items at the given
// indent. The head and foot comment runs come from the item
// that opens the block - the same attribution the parse gave
// it, reversed. The list's own fields pick the form: a
// Dashed list writes each item as a "- " line when every
// item sits on one line, and everything else - a fenced
// dashed list, or a dashed list holding a block item - renders
// each element's block between --- delimiters instead of
// guessing.
func writeBlockList(b *strings.Builder, l *List, indent string, head, foot Cmt) error {
	// The dashed form asks every item to sit on one line: a
	// block item cannot, and the render falls back to the ---
	// delimited form, which holds any element.
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
				// A keyed element in the fence form: the dash
				// spelling has no fence counterpart, so the
				// element renders as its own one-pair map.
				if err := writeMap(b, &Map{Items: []*Item{item}, Block: true}, indent, Cmt{}, Cmt{}); err != nil {
					return err
				}
			} else if !isBlock(item.Value) {
				// A scalar element: the value on the element's
				// only line, with its trailing comment after it.
				b.WriteString(indent)
				if err := writeValue(b, item.Value); err != nil {
					return err
				}
				if len(item.InCmt.Text) > 0 {
					b.WriteString(item.InCmt.Text[0])
				}
				b.WriteString("\n")
			} else if nested, ok := item.Value.(*List); ok && !nested.Dashed {
				// A nested list fence: the element opens one
				// indent under the fence, at the distance the
				// element's own item records. Dashed lists render
				// at the fence's indent.
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

// writeMap writes one map at the given indent, the prefix
// its pairs sit at: the opening item's Indent for a block
// value, empty at the top. The map's own Block picks the
// form: an inline map (Block unset) renders its one-line form.
// A block map's head and foot comment runs render verbatim:
// the head before the first item, the foot after the last,
// each item's pre and post around its line, and the in
// comment after its value.
//
// The blank divider between two non-empty runs is implicit:
// the parse drops it, and the container re-injects it here,
// one blank after each non-empty run that a comment follows.
// A blank stored inside a run - internal, or leading when the
// run above is empty - renders verbatim.
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

		// A value that cannot spell its recorded form in this
		// position takes the spelling the grammar allows. A
		// block map or dashed block list with no items has no
		// block form - the block opens on the lines under the
		// pair - so it respells as the inline empty container.
		// A fresh unquoted string with an inner newline opens
		// as a fence block, one line per line of content,
		// instead of one escaped line hiding every break. The
		// trailing newlines do not count - only a break
		// between content lines does. The parse never records
		// Unquoted in value position, so this only serves
		// trees built outside the parse - the JSON decode is
		// the main one.
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

		// The item's value is written once, in its recorded
		// form: an inline value on the pair's line with its
		// trailing comment, a block value opening at the
		// item's Indent - which holds any value, even one
		// without newlines, through the trailing backslash
		// continuation.
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
			// The item opens the block: the block sits at the
			// item's own indent, one step under the pair. A
			// synthetic tree records no indent, so the default
			// step is written: four spaces, the same one the
			// JSON decode uses for its containers.
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
		// An inline context asks the one-line spellings: a
		// fence-block record respells to its compact form,
		// which decodes to the same value.
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
		// The map's inline form holds only inline values: an
		// item claiming a block form inside it is a malformed
		// AST, and it is rejected rather than silently
		// normalized.
		for _, item := range n.Items {
			if isBlock(item.Value) {
				return fmt.Errorf("cannot render a block value inside an inline map")
			}
		}
		return writeMap(b, n, "", Cmt{}, Cmt{})
	case *List:
		// The list's inline form is the same: only inline
		// values inside, the same check as the map's.
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

// escapeDoubleFenced spells s inside a fence family: the closed
// escape set over the value, the quote kept raw - the
// delimiter is three quotes, so a lone quote needs no
// escape. The final-quote adjacency of the compact close is
// Quote's off-band tail, not an escape.
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

// resolveQuote picks the quote family that spells s in the
// asked shape: inline (a one-line spelling) or block (a
// fence block). The Unquoted ask is auto - a bare word is kept
// bare, anything else falls to the quoted families, double
// first, single only when the content holds a double quote or
// a backslash the single family spells raw - and an asked
// family that cannot hold s escalates to the cheapest one
// that can. The quoted families are inline-only: ' holds
// neither a quote nor a newline. A fence's capacity depends on
// the shape: the compact (inline) raw fence holds no newline,
// no interior delimiter, and no final quote - the close would
// swallow it into a typo; the block raw fence holds newlines,
// but needs a final newline, which the raw family cannot mark
// away, and no content line equal to its delimiter.
func resolveQuote(s string, q QuoteKind, block bool) QuoteKind {
	switch q {
	case Unquoted:
		if isBareWord(s) {
			break
		}
		q = DoubleQuoted
		if strings.ContainsRune(s, '"') || strings.ContainsRune(s, '\\') {
			// The single family spells both raw, the cleaner
			// spelling - when it holds the rest of the content.
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
			// The compact raw family holds neither a newline, an
			// interior delimiter, nor a final quote - the close
			// would swallow it into a four-quote typo.
			if strings.Contains(s, "'''") || strings.ContainsRune(s, '\n') ||
				strings.HasSuffix(s, "'") {
				q = DoubleFenced
			}
			break
		}
		// The block raw family holds newlines but cannot mark a
		// missing final one, and a content line equal to the
		// delimiter would close the fence early.
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

// quoteString writes one string spelled in the asked family,
// never failing: resolveQuote escalates any ask that cannot
// hold the string to the cheapest family that can. The force
// flag carries the position: true in value position, where a
// bare word is never read back and the bare spelling is
// skipped; false in key position, where a bare word recorded
// bare stays bare. The compact escaped fence keeps the quote
// raw, and a final bare quote escapes - the off-band tail of
// the compact close.
func quoteString(b *strings.Builder, s string, q QuoteKind, force bool) {
	q = resolveQuote(s, q, false)
	if force && q == Unquoted {
		// Value position: the ladder kept the word bare, a
		// spelling the grammar never reads back there. Double
		// first, single under the escape-worthy rule, the same
		// choice the ladder makes for a non-bare word.
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
		// The compact escaped family keeps the quote raw, but
		// a final raw quote would run into the closing
		// delimiter and the parser would read a typo, so the
		// final quote is escaped - the off-band tail of the
		// compact close.
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

// Quote spells one string in the asked family, returning the
// text. See quoteString for the spelling. The spelling is the
// key-position one: a bare word asked bare stays bare, the
// caller deciding whether that reading fits.
func Quote(s string, q QuoteKind) string {
	var b strings.Builder
	quoteString(&b, s, q, false)
	return b.String()
}
