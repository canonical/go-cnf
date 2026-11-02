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

// Package cnfast reads cnf files into an abstract syntax tree:
// the document as syntax, before interpretation. The AST records
// spellings and structure, not decoded values.
package cnfast

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// A line is one physical line of the document, split into its
// indentation prefix and its content.
type line struct {
	num    int    // 1-based line number, for errors
	indent string // the literal prefix, spaces or tabs
	text   string // content after the prefix
	raw    string // the whole line, prefix included
}

func splitLines(data string) []line {
	if data == "" {
		return nil
	}
	raws := strings.Split(data, "\n")
	if data[len(data)-1] == '\n' {
		raws = raws[:len(raws)-1] // the final newline ends the last line
	}
	lines := make([]line, len(raws))
	for n, raw := range raws {
		if len(raw) > 0 && raw[len(raw)-1] == '\r' {
			raw = raw[:len(raw)-1]
		}
		cut := 0
		for cut < len(raw) && (raw[cut] == ' ' || raw[cut] == '\t') {
			cut++
		}
		lines[n] = line{num: n + 1, indent: raw[:cut], text: raw[cut:], raw: raw}
	}
	return lines
}

type parser struct {
	lines   []line
	pos     int
	pending []line // comment and blank lines, not yet attributed
}

func errAt(pos Pos, msg string) error {
	return fmt.Errorf("%d:%d: %s", pos.L, pos.C, msg)
}

// errAtScan wraps a scan error at the source position where the
// scanned text begins, advanced over the text the offset names.
func errAtScan(pos Pos, text string, scanErr error) error {
	serr, ok := scanErr.(scanError)
	if !ok {
		return errAt(pos, scanErr.Error())
	}
	offset := serr.offset
	if offset > len(text) {
		offset = len(text)
	}
	delta := text[:offset]
	l := pos.L + strings.Count(delta, "\n")
	last := strings.LastIndexByte(delta, '\n')
	c := pos.C
	if last >= 0 {
		c = utf8.RuneCountInString(delta[last+1:]) + 1
	} else {
		c += utf8.RuneCountInString(delta)
	}
	return errAt(Pos{L: l, C: c}, serr.msg)
}

func indentPos(ln line) Pos {
	return Pos{L: ln.num, C: len(ln.indent) + 1}
}

// quoteShort quotes a short prefix of s for an error message:
// up to five runes as they stand, then "..." when more follow.
func quoteShort(s string) string {
	const cut = 5
	for i, _ := range s {
		if i == cut {
			s = s[:cut] + "..."
			break
		}
	}
	return "`" + Escape(s) + "`"
}

// Escape escapes s by the double-quote rule, without the quotes.
func Escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteString(escapeRune(r))
	}
	return b.String()
}

// keyPath spells one key path for an error message: dot-joined
// segments inside backquotes, each in its recorded quote style.
func keyPath(key []*Str) string {
	var b strings.Builder
	b.WriteString("`")
	for i, k := range key {
		if i > 0 {
			b.WriteString(".")
		}
		b.WriteString(Quote(k.Data, k.Quote))
	}
	b.WriteString("`")
	return b.String()
}

// ParseOptions are the settings of one Parse call. None exist
// yet; the parameter is there so that settings can be added
// later without another breaking change.
type ParseOptions struct{}

// Parse reads a cnf document into its AST.
func Parse(data []byte, opts *ParseOptions) (*Item, error) {
	if !utf8.Valid(data) {
		return nil, invalidUTF8Err(data)
	}
	p := &parser{lines: splitLines(string(data))}
	item, err := p.parseTop()
	if err != nil {
		return nil, err
	}
	if err := checkConflicts(item); err != nil {
		return nil, err
	}
	return item, nil
}

// invalidUTF8Err locates the first invalid byte in data and
// names its line and column, both 1-based, in runes.
func invalidUTF8Err(data []byte) error {
	line, col, off := 1, 1, 0
	for i := 0; i < len(data); {
		if data[i] == '\n' {
			line, col, off = line+1, 1, i+1
			i++
			continue
		}
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size <= 1 {
			col += utf8.RuneCount(data[off:i])
			return errAt(Pos{L: line, C: col}, fmt.Sprintf("cannot decode byte %#x: not UTF-8", data[i]))
		}
		i += size
	}
	// Unreachable: the caller only asks on invalid input.
	return fmt.Errorf("cannot decode document: not UTF-8")
}

// parseTop reads the file's one value, always wrapped in a
// keyless root Item. No structural line may follow the value.
func (p *parser) parseTop() (*Item, error) {
	first, ok := p.peekStruct("")
	if err := cmtIndentOK(p.pending, ""); err != nil {
		return nil, err
	}
	item := &Item{}
	if !ok {
		// An empty file is an empty map, written as a block.
		item.Value = &Map{Items: []*Item{}, Block: true}
		item.HeadCmt = cmtOf(p.pending)
		p.pending = nil
		return item, nil
	}
	var err error
	if looksLikeItem(first.text) {
		// A pair-shaped line opens a map; parseMap divides the
		// pending run between the map's head and the first pre.
		item.Value, err = p.parseMap(item, "")
		if err != nil {
			return nil, err
		}
		item.Value.(*Map).Block = true
	} else {
		// Any other value opens on its own line at column zero.
		item.PreCmt = cmtOf(p.pending)
		p.pending = nil
		if first.indent != "" {
			return nil, errAt(indentPos(first), "cannot indent a top-level value")
		}
		item.Value, err = p.parseValue(item, first)
	}
	if err != nil {
		return nil, err
	}
	if ln, more := p.peekStruct(""); more {
		return nil, errAt(indentPos(ln), "cannot parse content after top-level value: "+quoteShort(ln.text))
	}
	item.PostCmt = cmtOf(p.pending)
	p.pending = nil
	return item, nil
}

// looksLikeItem is a cheap check for a "path: value" line;
// the real scan names the path, not the value, on a bad line.
func looksLikeItem(text string) bool {
	i := strings.IndexByte(text, ':')
	if i <= 0 {
		return false
	}
	c := text[0]
	return c == '"' || c == '\'' || c == '.' || isBareRune(rune(c))
}

// parseMap reads one block map's pairs at the exact prefix. A run splits
// at its first blank; without one it travels forward whole. A shallower
// comment closes the block; a deeper one errors unless the pair is on it.
func (p *parser) parseMap(item *Item, parentIndent string) (*Map, error) {
	m := &Map{Items: []*Item{}} // an empty map keeps its items [], never null
	for {
		ln, ok := p.peekStruct(parentIndent)
		if !ok {
			if err := p.cmtAssignPost(item, m.Items, parentIndent); err != nil {
				return nil, err
			}
			return m, nil
		}
		if ln.indent != parentIndent {
			if len(ln.indent) > len(parentIndent) {
				// The pair that misses the block's prefix is named at
				// the deeper line's own start.
				return nil, errAt(indentPos(ln), "cannot mix indentation prefixes")
			}
			// A shallower line - structural or comment - closes the
			// block; it stays in the stream for the parent.
			if err := p.cmtAssignPost(item, m.Items, parentIndent); err != nil {
				return nil, err
			}
			return m, nil
		}
		if len(m.Items) == 0 {
			m.Pos = Pos{L: ln.num, C: len(ln.indent) + 1}
		}
		pre, err := p.cmtSplitPre(item, m.Items, parentIndent)
		if err != nil {
			return nil, err
		}
		p.pos++
		item, err := p.parseItem(ln)
		if err != nil {
			return nil, err
		}
		item.PreCmt = pre
		m.Items = append(m.Items, item)
	}
}

// cmtSplitPre resolves the pending run at an item line of the block,
// returning the item's pre: the previous post stops at the first blank,
// the first pre divides from the head at the last.
func (p *parser) cmtSplitPre(owner *Item, items []*Item, parent string) (Cmt, error) {
	run := p.pending
	p.pending = nil
	if len(run) == 0 {
		return Cmt{}, nil
	}
	if err := cmtIndentOK(run, parent); err != nil {
		return Cmt{}, err
	}
	if len(items) == 0 {
		if i := lastBlank(run); i >= 0 {
			owner.HeadCmt = cmtOf(run[:i])
			if i > 0 {
				return cmtOf(run[i+1:]), nil
			}
			return cmtOf(run), nil
		}
		return cmtOf(run), nil // tie-break: no blank, all forward
	}
	if i := firstBlank(run); i >= 0 {
		items[len(items)-1].PostCmt = cmtOf(run[:i])
		if i > 0 {
			return cmtOf(run[i+1:]), nil
		}
		return cmtOf(run), nil
	}
	return cmtOf(run), nil // tie-break: no blank, all forward
}

// cmtAssignPost resolves the pending run at the block's close: the run
// before the first blank is the last item's post, the blank onward the
// block's foot. Without a blank the run hugs the last item.
func (p *parser) cmtAssignPost(owner *Item, items []*Item, parent string) error {
	run := p.pending
	p.pending = nil
	if len(run) == 0 {
		return nil
	}
	if err := cmtIndentOK(run, parent); err != nil {
		return err
	}
	if len(items) == 0 {
		owner.HeadCmt = cmtOf(run) // no items: the run precedes them all
		return nil
	}
	last := items[len(items)-1]
	if inner, ok := last.Value.(*Map); ok && endsBlank(last, inner) {
		// The item's block map ended on a blank: the run cannot
		// be its post, and is the block's foot.
		owner.FootCmt = cmtOf(run)
		return nil
	}
	if i := firstBlank(run); i >= 0 {
		last.PostCmt = cmtOf(run[:i])
		foot := run[i:]
		if !hasComment(foot) {
			// Blanks only: the divider, not a run - the parent collects
			// it as the leading blank of its own next run.
			p.pending = foot
			return nil
		}
		if i > 0 {
			// The blank at i divides the two runs.
			owner.FootCmt = cmtOf(run[i+1:])
			return nil
		}
		owner.FootCmt = cmtOf(foot)
		return nil
	}
	// Tie-break: no blank, the run hugs the last item.
	last.PostCmt = cmtOf(run)
	return nil
}

// hasComment reports whether the run holds at least one
// comment line, as opposed to only blanks.
func hasComment(run []line) bool {
	for _, ln := range run {
		if ln.text != "" {
			return true
		}
	}
	return false
}

// endsBlank reports whether the item's block map ends with a
// blank line, which divides the block from what follows it.
func endsBlank(item *Item, m *Map) bool {
	tail := item.FootCmt.Text
	if len(tail) == 0 && len(m.Items) == 0 {
		tail = item.HeadCmt.Text
	}
	return len(tail) > 0 && tail[len(tail)-1] == ""
}

// cmtIndentOK reports an error when a comment line in the
// run does not sit at the given prefix.
func cmtIndentOK(run []line, parent string) error {
	for _, ln := range run {
		if ln.text != "" && ln.indent != parent {
			return errAt(indentPos(ln), "cannot mix indentation prefixes")
		}
	}
	return nil
}

// cmtOf builds one comment run from its lines: one element
// per line, the content after the indent - the render re-adds
// the indent. A blank line is the empty string.
func cmtOf(run []line) Cmt {
	if len(run) == 0 {
		return Cmt{}
	}
	lines := make([]string, len(run))
	for i, ln := range run {
		lines[i] = ln.text
	}
	return Cmt{Text: lines, Pos: Pos{L: run[0].num, C: len(run[0].indent) + 1}}
}

func firstBlank(run []line) int {
	for i, ln := range run {
		if ln.text == "" {
			return i
		}
	}
	return -1
}

func lastBlank(run []line) int {
	last := -1
	for i, ln := range run {
		if ln.text == "" {
			last = i
		}
	}
	return last
}

// peekStruct returns the next structural line, collecting the blank and
// comment lines it skips into the pending run. A shallower comment closes
// the block, returned unconsumed; blanks never close one.
func (p *parser) peekStruct(prefix string) (line, bool) {
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.text == "" {
			p.pending = append(p.pending, ln)
			p.pos++
			continue
		}
		if strings.HasPrefix(ln.text, "#") {
			if len(ln.indent) < len(prefix) {
				return ln, true
			}
			p.pending = append(p.pending, ln)
			p.pos++
			continue
		}
		return ln, true
	}
	return line{}, false
}

// parseItem splits one "path: value" line. A token at offset i in the
// line's text sits at column len(indent)+1+i. A block opened by the lines
// that follow records its distance on the item's Indent.
func (p *parser) parseItem(ln line) (*Item, error) {
	base := len(ln.indent) + 1
	path, after, err := scanKeyPath(ln.text)
	if err != nil {
		return nil, errAtScan(Pos{L: ln.num, C: base}, ln.text, err)
	}
	if after == "" || after[0] != ':' {
		// The separator must sit where the scan stopped.
		return nil, errAt(Pos{L: ln.num, C: base + len(ln.text) - len(after)}, "cannot find key-value separator `:`")
	}
	for i := range path {
		path[i].Pos.L = ln.num
		path[i].Pos.C = base + path[i].offset
	}
	// The grammar's mapvalue: an inline value on the line, or a
	// block opened by the lines that follow, one indent under
	// the pair.
	item := &Item{Key: path}
	text := strings.TrimLeft(after[1:], " \t")
	var in Cmt
	if text == "" {
		next, ok := p.peekStruct(ln.indent)
		if !ok || len(next.indent) <= len(ln.indent) {
			return nil, errAt(indentPos(ln), "cannot omit the value for "+keyPath(path))
		}
		// The block's shape is decided by its first line: a
		// "- " item opens a list, a fence delimiter a fence,
		// a --- line a list fence, anything else a map.
		switch {
		case isListItem(next.text):
			item.Value, err = p.parseBlockList(item, next.indent)
		case isStringFence(next.text) || next.text == "---":
			// Nothing may sit between the pair and its fence;
			// parseValue routes the two fence forms.
			if err := p.cmtNoFencePrefix(); err != nil {
				return nil, err
			}
			item.Value, err = p.parseValue(item, next)
		default:
			item.Value, err = p.parseMap(item, next.indent)
			if err != nil {
				return nil, err
			}
			item.Value.(*Map).Block = true
		}
		if err != nil {
			return nil, err
		}
		// The block opens under the pair: the item records the
		// distance, the pair's own and no sibling's.
		item.Indent = next.indent[len(ln.indent):]
		return item, nil
	}
	item.Value, in, err = p.parseInline(ln, text)
	item.InCmt = in
	if err != nil {
		return nil, err
	}
	return item, nil
}

// isListItem reports whether a line's text opens a block list item:
// a "-" followed by a blank or the end of the text.
func isListItem(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "-\t")
}

// isStringFence reports whether text is a fence delimiter.
func isStringFence(text string) bool {
	return text == `"""` || text == `'''`
}

// parseValue reads the value opening at first, a peeked line: a fence,
// a list fence, a dashed list, a pair-shaped block map, or an inline
// value. The block under a pair is parseItem's, its map default its own.
func (p *parser) parseValue(owner *Item, first line) (Value, error) {
	switch {
	case isStringFence(first.text):
		return p.parseFence(first, first.text)
	case first.text == "---":
		return p.parseListFence(first)
	case isListItem(first.text):
		return p.parseBlockList(owner, first.indent)
	case looksLikeItem(first.text):
		v, err := p.parseMap(owner, first.indent)
		if err != nil {
			return nil, err
		}
		v.Block = true
		return v, nil
	default:
		// An inline value on its only line.
		p.pos++ // consume the value's own line
		v, in, err := p.parseInline(first, first.text)
		owner.InCmt = in
		return v, err
	}
}

// parseBlockList reads the "- item" lines of one block list
// at the exact prefix. Each item is the grammar's inlinepair:
// a key when the item line carries a path, absent otherwise.
func (p *parser) parseBlockList(owner *Item, prefix string) (*List, error) {
	items := []*Item{}
	pos := Pos{}
	first := true
	for {
		ln, ok := p.peekStruct(prefix)
		if !ok || ln.indent != prefix || !isListItem(ln.text) {
			if err := p.cmtAssignPost(owner, items, prefix); err != nil {
				return nil, err
			}
			l := &List{Items: items, Block: true, Dashed: true, Pos: pos}
			return l, nil
		}
		if first {
			pos = Pos{L: ln.num, C: len(ln.indent) + 1}
			first = false
		}
		pre, err := p.cmtSplitPre(owner, items, prefix)
		if err != nil {
			return nil, err
		}
		p.pos++
		text := strings.TrimLeft(ln.text[1:], " \t")
		if text == "" {
			// A dash with nothing after it.
			return nil, errAt(indentPos(ln), "cannot omit the value")
		}
		item := &Item{}
		item.PreCmt = pre
		// The inlinepair form: a path and a colon before the
		// value; without them the whole text is the value.
		if path, after, kerr := scanKeyPath(text); kerr == nil && after != "" && after[0] == ':' {
			off := len(ln.text) - len(text)
			for i := range path {
				path[i].Pos.L = ln.num
				path[i].Pos.C = len(ln.indent) + 1 + off + path[i].offset
			}
			item.Key = path
			text = strings.TrimLeft(after[1:], " \t")
			if text == "" {
				// The inlinepair form ends on its colon: the same
				// omission parseItem raises.
				return nil, errAt(indentPos(ln), "cannot omit the value for "+keyPath(path))
			}
		}
		val, in, err := p.parseInline(ln, text)
		if err != nil {
			return nil, err
		}
		item.Value = val
		item.InCmt = in
		items = append(items, item)
	}
}

// parseFence reads one fence block from its opening line, already peeked
// as next. The close lands only at the exact prefix; a shallower blank
// is the newline only; deeper lines' excess indent is content.
func (p *parser) parseFence(next line, delim string) (Value, error) {
	prefix := next.indent
	p.pos++ // consume the opening delimiter
	quote := SingleFenced
	if delim == `"""` {
		quote = DoubleFenced
	}
	s := &Str{Quote: quote, Block: true, Pos: Pos{L: next.num, C: len(prefix) + 1}}
	escaped := delim == `"""`
	indentChar := byte(' ')
	if len(prefix) > 0 {
		indentChar = prefix[0]
	}
	var b strings.Builder
	var text []string // the encoded lines, one per source line
	continued := false
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		p.pos++
		// The closing delimiter always closes the fence, even
		// when the previous line continued: the trailing escape
		// ate the newline, so the value ends without one.
		if ln.indent == prefix && ln.text == delim {
			s.Data = b.String()
			s.Text = text
			return s, nil
		}
		if !continued && !sameIndentChar(ln.indent, indentChar) {
			return nil, errAt(indentPos(ln), "cannot mix indentation prefixes in fence")
		}
		if len(ln.indent) < len(prefix) {
			if ln.text != "" {
				// Only the exact prefix closes the fence.
				return nil, errAt(indentPos(ln), "cannot close fence at shallower prefix")
			}
			// A shallower blank contributes the newline only.
			b.WriteString("\n")
			text = append(text, "\n")
			continue
		}
		if ln.text == "" {
			// A blank at or deeper than the fence prefix: its
			// whitespace is content.
			b.WriteString(ln.indent[len(prefix):])
			b.WriteString("\n")
			text = append(text, ln.indent[len(prefix):]+"\n")
			continue
		}
		if !strings.HasPrefix(ln.indent, prefix) {
			return nil, errAt(indentPos(ln), "cannot mix indentation prefixes in fence")
		}
		// The content following the valid prefix is the string,
		// including any excess indentation on deeper lines.
		content := ln.indent[len(prefix):] + ln.text
		contentPos := Pos{L: ln.num, C: len(prefix) + 1}
		continued = false
		if escaped && oddTrailingBackslashes(content) {
			content = content[:len(content)-1]
			continued = true
		}
		// The recorded element keeps the source's own tail: the
		// terminator, or the continuation marker.
		encoded := content
		if escaped {
			unq, err := unescape(content)
			if err != nil {
				return nil, errAtScan(contentPos, content, err)
			}
			content = unq
		}
		if continued {
			encoded += `\`
		} else {
			encoded += "\n"
		}
		b.WriteString(content)
		text = append(text, encoded)
		if !continued {
			b.WriteString("\n")
		}
	}
	return nil, errAt(indentPos(next), "cannot find closing fence")
}

// sameIndentChar reports whether the prefix consists solely of
// the given indent character.
func sameIndentChar(prefix string, c byte) bool {
	for i := 0; i < len(prefix); i++ {
		if prefix[i] != c {
			return false
		}
	}
	return true
}

// oddTrailingBackslashes reports whether the text ends on an
// odd run of backslashes - an escape that ate the newline.
func oddTrailingBackslashes(s string) bool {
	n := 0
	for n < len(s) && s[len(s)-1-n] == '\\' {
		n++
	}
	return n%2 == 1
}

// unescape processes the closed escape set over raw text. Its errors
// carry the backslash's offset within s, to name the exact column.
func unescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			return "", scanError{i, "cannot end line on an escape"}
		}
		r, n, err := scanEscape(s[i:])
		if err != nil {
			return "", scanError{i, err.Error()}
		}
		b.WriteRune(r)
		i += n
	}
	return b.String(), nil
}

// parseListFence reads one --- delimited list from its opening line,
// already peeked as next. Each element is the lines between two ---
// delimiters at the fence's exact prefix, parsed as a block value.
func (p *parser) parseListFence(next line) (Value, error) {
	prefix := next.indent
	p.pos++ // consume the opening ---
	l := &List{Items: []*Item{}, Block: true, Pos: Pos{L: next.num, C: len(prefix) + 1}}
	nested := "" // the indent of the fence's nested elements, once set
	for {
		start := p.pos
		closed := false
		for p.pos < len(p.lines) {
			cur := p.lines[p.pos]
			if cur.indent == prefix && cur.text == "---" {
				closed = true
				break
			}
			p.pos++
		}
		if !closed {
			// The fence that never closes is named where it
			// opened: its own position.
			return nil, errAt(indentPos(next), "cannot find closing list fence")
		}
		elem := p.lines[start:p.pos]
		if len(elem) == 0 {
			// The error names the delimiter that opened the
			// element: the previous element's close, or the
			// fence's own opening line.
			open := next
			if start > 0 {
				open = p.lines[start-1]
			}
			return nil, errAt(indentPos(open), "cannot parse empty list fence element")
		}
		// Nested fences must share one indent: the first one's opening
		// --- sets it.
		if elem[0].text == "---" && len(elem[0].indent) > len(prefix) {
			if nested == "" {
				nested = elem[0].indent
			} else if elem[0].indent != nested {
				return nil, errAt(indentPos(elem[0]), "cannot mix indents inside list fence")
			}
		}
		p.pos++
		sub := &parser{lines: elem}
		// The element may open with comment and blank lines; the
		// sub-parser's peek collects them into its pending run.
		first, ok := sub.peekStruct(prefix)
		if !ok {
			open := next
			if start > 0 {
				open = p.lines[start-1]
			}
			return nil, errAt(indentPos(open), "cannot parse empty list fence element")
		}
		eitem := &Item{}
		if nested != "" {
			// A nested fence sits one indent deeper than the
			// fence's delimiters; a flat element adds nothing.
			eitem.Indent = nested[len(prefix):]
		}
		val, err := sub.parseListFenceValue(eitem, first, prefix)
		if err != nil {
			return nil, err
		}
		if sub.pos != len(sub.lines) {
			// The leftover line at its own start: the content
			// that comes after the element's value.
			return nil, errAt(indentPos(sub.lines[sub.pos]), "cannot parse content in list fence element")
		}
		eitem.Value = val
		l.Items = append(l.Items, eitem)
		// The fence continues while the next structural line sits
		// at its prefix or deeper; a shallower line closes it
		// and stays in the stream for the parent.
		ln, ok := p.peekStruct(prefix)
		if !ok || len(ln.indent) < len(prefix) {
			return l, nil
		}
	}
}

// parseListFenceValue reads one element of a list fence from
// its first line, already peeked: a value at the fence's
// prefix, or a nested list fence one indent deeper.
func (p *parser) parseListFenceValue(item *Item, first line, fence string) (Value, error) {
	if first.indent == fence {
		return p.parseValue(item, first)
	}
	if first.text == "---" && len(first.indent) > len(fence) {
		// A nested list fence: the grammar's INDENT listfence
		// DEDENT - deeper than the enclosing fence's delimiters.
		return p.parseListFence(first)
	}
	// A dashed list, a map, or an inline item at a deeper
	// prefix: only a nested list fence opens a deeper block
	// inside a fence.
	return nil, errAt(indentPos(first), "list element indent must align with fence")
}

// cmtNoFencePrefix rejects a pending run before a fence: the
// fence must open at the very next line after its pair.
func (p *parser) cmtNoFencePrefix() error {
	for _, ln := range p.pending {
		if ln.text != "" {
			return errAt(indentPos(ln), "cannot comment between key and fence")
		}
		return errAt(indentPos(ln), "cannot blank between key and fence")
	}
	return nil
}

// parseInline reads one inline value on the item's line. It may
// carry a trailing comment, returned verbatim from its first blank.
func (p *parser) parseInline(ln line, text string) (Value, Cmt, error) {
	val, rest, err := p.scanValueAt(ln, text)
	if err != nil {
		return nil, Cmt{}, err
	}
	left, cmt, ok := stripTrail(rest)
	if ok {
		col := len(ln.indent) + 1 + len(ln.text) - len(rest)
		return val, Cmt{Text: []string{cmt}, Pos: Pos{L: ln.num, C: col}}, nil
	}
	if left != "" {
		// The leftover sits at the line's tail, at its own first column.
		return nil, Cmt{}, errAt(Pos{L: ln.num, C: len(ln.indent) + 1 + len(ln.text) - len(left)}, fmt.Sprintf("cannot parse trailing `%s`", Escape(left)))
	}
	return val, Cmt{}, nil
}

// stripTrail splits a trailing comment - at least one blank,
// then # - off the rest of a value. The blank is required:
// a # glued to the value is content.
func stripTrail(rest string) (left, cmt string, ok bool) {
	i := 0
	for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
		i++
	}
	if i == 0 || i >= len(rest) || rest[i] != '#' {
		return rest, "", false
	}
	return "", rest, true
}

// scanValueAt reads one inline value from the front of text,
// returning the value and the unread remainder.
func (p *parser) scanValueAt(ln line, text string) (Value, string, error) {
	col := len(ln.text) - len(text) + len(ln.indent) + 1
	pos := Pos{L: ln.num, C: col}
	if text == "" {
		// An empty scan: the caller's text is spent.
		return nil, "", errAt(pos, "cannot parse value at \"\"")
	}
	switch text[0] {
	case '{':
		return p.scanInlineMap(ln, text, pos)
	case '[':
		return p.scanInlineList(ln, text, pos)
	case '"':
		if strings.HasPrefix(text, `"""`) {
			return p.scanFencedLine(ln, text, `"`, pos)
		}
		s, rest, err := scanDoubleQuoted(text)
		if err != nil {
			return nil, "", errAtScan(pos, text, err)
		}
		return &Str{Data: s, Quote: DoubleQuoted, Pos: pos}, rest, nil
	case '\'':
		if strings.HasPrefix(text, `'''`) {
			return p.scanFencedLine(ln, text, `'`, pos)
		}
		s, rest, err := scanSingleQuoted(text)
		if err != nil {
			return nil, "", errAt(pos, err.Error())
		}
		return &Str{Data: s, Quote: SingleQuoted, Pos: pos}, rest, nil
	}
	tok, rest := scanScalarToken(text)
	switch tok {
	case "null", "true", "false":
		return &Scalar{Text: tok, Pos: pos}, rest, nil
	case "":
		// The text starts outside every scanned class.
		return nil, "", errAt(pos, fmt.Sprintf("cannot parse value at `%s`", Escape(text)))
	}
	// A bare word is never a value: the token must be
	// number-shaped to be a Num.
	body := strings.TrimPrefix(tok, "-")
	if body == "" || body[0] < '0' || body[0] > '9' {
		return nil, "", errAt(pos, fmt.Sprintf("cannot parse unquoted value `%s`", Escape(tok)))
	}
	// The base prefixes carry their own digit grammar.
	for _, p := range []struct {
		prefix string
		base   int
	}{
		{"0x", 16}, {"0o", 8}, {"0b", 2},
	} {
		if strings.HasPrefix(body, p.prefix) {
			digits := body[len(p.prefix):]
			if digits == "" {
				return nil, "", errAt(pos, fmt.Sprintf("cannot parse number `%s`", Escape(tok)))
			}
			for _, c := range digits {
				if digitVal(c) < 0 || digitVal(c) >= p.base {
					return nil, "", errAt(pos, fmt.Sprintf("cannot parse number `%s`", Escape(tok)))
				}
			}
			return &Num{Text: tok, Pos: pos}, rest, nil
		}
	}
	// The grammar is lowercase-only: uppercase exponents are
	// errors, and so are redundant leading zeros.
	if strings.ContainsRune(body, 'E') {
		return nil, "", errAt(pos, fmt.Sprintf("cannot parse number `%s`: uppercase exponent", Escape(tok)))
	}
	if len(body) > 1 && body[0] == '0' && body[1] >= '0' && body[1] <= '9' {
		return nil, "", errAt(pos, fmt.Sprintf("cannot parse number `%s`: redundant leading zero", Escape(tok)))
	}
	if !isDecimalShape(body) {
		return nil, "", errAt(pos, fmt.Sprintf("cannot parse number `%s`", Escape(tok)))
	}
	return &Num{Text: tok, Pos: pos}, rest, nil
}

// isDecimalShape matches the grammar's decimal spellings:
// numint [ "." 1*DIGIT ] [ "e" ["+" / "-"] 1*DIGIT ]. The redundant
// leading zero is checked separately, for its own message.
func isDecimalShape(body string) bool {
	mantissa := body
	if i := strings.IndexByte(body, 'e'); i >= 0 {
		mantissa = body[:i]
		exp := body[i+1:]
		if exp == "" {
			return false
		}
		if exp[0] == '+' || exp[0] == '-' {
			exp = exp[1:]
		}
		if !allDigits(exp) {
			return false
		}
	}
	intPart := mantissa
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		intPart = mantissa[:i]
		if !allDigits(mantissa[i+1:]) {
			return false
		}
	}
	return intPart != "" && allDigits(intPart)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// scanScalarToken reads the maximal token that could spell
// a scalar; the caller tells number, keyword, or bare word.
func scanScalarToken(text string) (tok, rest string) {
	cut := 0
	if cut < len(text) && text[cut] == '-' {
		cut++
	}
	for cut < len(text) {
		c := text[cut]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '.':
			cut++
		case (c == '+' || c == '-') && cut > 0 && (text[cut-1] == 'e' || text[cut-1] == 'E'):
			cut++
		default:
			return text[:cut], text[cut:]
		}
	}
	return text, ""
}

// scanFencedLine reads a one-line fence: the first delimiter
// closes, and the escaped family's search skips escapes.
// A stray quote after the close is a typo, not content.
func (p *parser) scanFencedLine(ln line, text, quote string, pos Pos) (Value, string, error) {
	delim := strings.Repeat(quote, 3)
	q := SingleFenced
	if quote == `"` {
		q = DoubleFenced
	}
	end := -1
	if quote == `"` {
		i := 3
		for i <= len(text)-3 {
			if text[i] == '\\' {
				i += 2 // skip the escape; its quote is content
				continue
			}
			if text[i:i+3] == delim {
				end = i - 3
				break
			}
			i++
		}
	} else {
		end = strings.Index(text[3:], delim)
	}
	if end < 0 {
		return nil, "", errAt(pos, "cannot find closing fence")
	}
	inner := text[3 : 3+end]
	innerPos := Pos{L: pos.L, C: pos.C + 3}
	rest := text[3+end+3:]
	if quote == `"` {
		unq, err := unescape(inner)
		if err != nil {
			// The interior starts three quotes past the token start.
			return nil, "", errAtScan(innerPos, inner, err)
		}
		// The compact interior records as one element: the close
		// adds neither a marker nor a terminator.
		return &Str{Data: unq, Text: []string{inner}, Quote: q, Pos: pos}, rest, nil
	}
	// The raw family holds the interior as it stands.
	return &Str{Data: inner, Text: []string{inner}, Quote: q, Pos: pos}, rest, nil
}

// scanInlineList reads "[1, 2]" in the one-line form. Elements
// are inline values, recursing through scanValueAt, so nested
// lists work with no extra code.
func (p *parser) scanInlineList(ln line, text string, pos Pos) (Value, string, error) {
	l := &List{Items: []*Item{}, Pos: pos} // inline: the zero form
	text = strings.TrimLeft(text[1:], " \t")
	if strings.HasPrefix(text, "]") {
		return l, text[1:], nil
	}
	for {
		val, rest, err := p.scanValueAt(ln, text)
		if err != nil {
			return nil, "", err
		}
		l.Items = append(l.Items, &Item{Value: val})
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			// The list that never closes is named where it
			// opened: its own start, so nested lists tell apart.
			return nil, "", errAt(pos, "cannot find closing `]`")
		}
		if rest[0] == ']' {
			return l, rest[1:], nil
		}
		if rest[0] != ',' {
			// The leftover at its own first column: the honest
			// spot for what refuses to be a separator.
			return nil, "", errAt(Pos{L: ln.num, C: len(ln.indent) + 1 + len(ln.text) - len(rest)}, fmt.Sprintf("cannot parse `%s` inside list", Escape(rest)))
		}
		text = strings.TrimLeft(rest[1:], " \t")
	}
}

// scanInlineMap reads "{ a: 1, b: 2 }" in the native item
// grammar, producing an inline-form Map.
func (p *parser) scanInlineMap(ln line, text string, pos Pos) (Value, string, error) {
	m := &Map{Items: []*Item{}, Pos: pos}
	text = strings.TrimLeft(text[1:], " \t")
	if strings.HasPrefix(text, "}") {
		return m, text[1:], nil
	}
	for {
		off := len(ln.text) - len(text)
		path, after, err := scanKeyPath(text)
		if err != nil {
			// The scan started at the current text's column;
			// the scan error's offset lands on the offender.
			return nil, "", errAtScan(Pos{L: ln.num, C: len(ln.indent) + 1 + off}, text, err)
		}
		if after == "" || after[0] != ':' {
			// The separator must sit where the scan stopped: the
			// remainder's first column, the honest spot for what
			// refuses to be one.
			return nil, "", errAt(Pos{L: ln.num, C: len(ln.indent) + 1 + off + len(text) - len(after)}, "cannot find key-value separator `:`")
		}
		for i := range path {
			path[i].Pos.L = ln.num
			path[i].Pos.C = len(ln.indent) + 1 + off + path[i].offset
		}
		val, rest, err := p.scanValueAt(ln, strings.TrimLeft(after[1:], " \t"))
		if err != nil {
			return nil, "", err
		}
		m.Items = append(m.Items, &Item{Key: path, Value: val})
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			// Named where it opened, so nested maps tell apart.
			return nil, "", errAt(pos, "cannot find closing `}`")
		}
		if rest[0] == '}' {
			return m, rest[1:], nil
		}
		if rest[0] != ',' {
			// The leftover at its own first column: the honest
			// spot for what refuses to be a separator.
			return nil, "", errAt(Pos{L: ln.num, C: len(ln.indent) + 1 + len(ln.text) - len(rest)}, fmt.Sprintf("cannot parse `%s` inside map", Escape(rest)))
		}
		text = strings.TrimLeft(rest[1:], " \t")
	}
}

// A scanError is an error a byte scanner raises inside the
// text it was scanning: the offset names where the offender
// sits within that text.
type scanError struct {
	offset int
	msg    string
}

func (e scanError) Error() string { return e.msg }

// scanKeyPath reads a dotted path of key segments, recording each
// segment's offset in the text for position tracking. Spaces are
// rejected: between segments, after the dot, or before the colon.
func scanKeyPath(text string) ([]*Str, string, error) {
	if text == "" || text[0] == '.' {
		return nil, "", scanError{0, "cannot parse empty key segment"}
	}
	var segs []*Str
	consumed := 0
	for {
		seg, after, err := scanKey(text)
		if err != nil {
			// A segment scanner error names its spot within the
			// segment; the path advances it by what came before.
			if serr, ok := err.(scanError); ok {
				serr.offset += consumed
				return nil, "", serr
			}
			return nil, "", err
		}
		seg.offset = consumed
		segs = append(segs, seg)
		consumed += len(text) - len(after)
		if after != "" && after[0] == '.' {
			if len(after) == 1 {
				return nil, "", scanError{consumed + 1, "cannot parse empty key segment"}
			}
			if after[1] == ' ' || after[1] == '\t' {
				return nil, "", scanError{consumed + 1, "cannot parse space in path"}
			}
			text = after[1:]
			consumed++
			continue
		}
		if after != "" && (after[0] == ' ' || after[0] == '\t') {
			return nil, "", scanError{consumed, "cannot parse space in path"}
		}
		return segs, after, nil
	}
}

// scanKey reads one key segment: a bare word or a quoted string.
func scanKey(text string) (*Str, string, error) {
	if text == "" {
		return nil, "", scanError{0, "cannot parse empty key segment"}
	}
	switch text[0] {
	case '"':
		s, rest, err := scanDoubleQuoted(text)
		if err != nil {
			return nil, "", err
		}
		return &Str{Data: s, Quote: DoubleQuoted}, rest, nil
	case '\'':
		s, rest, err := scanSingleQuoted(text)
		if err != nil {
			return nil, "", err
		}
		return &Str{Data: s, Quote: SingleQuoted}, rest, nil
	}
	seg, rest := scanWord(text)
	if seg == "" {
		return nil, "", scanError{0, "cannot parse empty key segment"}
	}
	for _, r := range seg {
		if !isBareRune(r) {
			// A bad word is named at its start.
			return nil, "", scanError{0, fmt.Sprintf("cannot parse key `%s`", Escape(seg))}
		}
	}
	return &Str{Data: seg, Quote: Unquoted}, rest, nil
}

// scanDoubleQuoted reads a double-quoted string, processing
// escapes. Its escape errors carry the backslash's offset
// within the token, for the caller to name the exact column.
func scanDoubleQuoted(text string) (string, string, error) {
	var b strings.Builder
	i := 1
	for i < len(text) {
		c := text[i]
		switch {
		case c == '"':
			return b.String(), text[i+1:], nil
		case c == '\\':
			if i+1 >= len(text) {
				return "", "", scanError{i, "cannot end string on an escape"}
			}
			r, n, err := scanEscape(text[i:])
			if err != nil {
				return "", "", scanError{i, err.Error()}
			}
			b.WriteRune(r)
			i += n
		default:
			b.WriteByte(c)
			i++
		}
	}
	return "", "", fmt.Errorf("cannot find closing quote")
}

// scanSingleQuoted reads a single-quoted string: raw, no escapes.
func scanSingleQuoted(text string) (string, string, error) {
	end := strings.IndexByte(text[1:], '\'')
	if end < 0 {
		return "", "", fmt.Errorf("cannot find closing quote")
	}
	return text[1 : 1+end], text[2+end:], nil
}

// scanEscape reads one escaped character.
func scanEscape(text string) (rune, int, error) {
	if len(text) < 2 || text[0] != '\\' {
		return 0, 0, fmt.Errorf("cannot parse escape")
	}
	c := text[1]
	switch c {
	case '"', '\\':
		return rune(c), 2, nil
	case 'n':
		return '\n', 2, nil
	case 'r':
		return '\r', 2, nil
	case 't':
		return '\t', 2, nil
	case 'b':
		return '\b', 2, nil
	case 'f':
		return '\f', 2, nil
	case 'u':
		return scanUnicode(text)
	case 'x':
		// Two hex digits must follow: text[2:4] slices them,
		// so four bytes must be present.
		if len(text) >= 4 {
			if v, err := parseHex(text[2:4]); err == nil {
				return rune(v), 4, nil
			}
		}
		return 0, 0, fmt.Errorf("cannot parse \\x escape")
	}
	return 0, 0, fmt.Errorf("cannot parse escape \\%c", c)
}

// scanUnicode reads one \u escape, both \u0000 and \u{...}.
func scanUnicode(text string) (r rune, n int, err error) {
	if len(text) >= 5 && text[2] == '{' {
		// \u{0}
		i := strings.IndexByte(text, '}')
		if i > 3 && i < 10 {
			n = i + 1
			text = text[3:i]
		}
	} else if len(text) >= 6 {
		// \u0000
		n = 6
		text = text[2:6]
	}
	if n > 0 {
		v, err := parseHex(text)
		if err == nil && isUnicodeEscape(v) {
			return rune(v), n, nil
		}
	}
	return 0, 0, fmt.Errorf("cannot parse \\u escape")
}

func isUnicodeEscape(v int64) bool {
	return v <= 0x10FFFF && (v < 0xD800 || v > 0xDFFF)
}

func parseHex(s string) (int64, error) {
	var v int64
	for i := 0; i < len(s); i++ {
		var d int64
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			d = int64(c - '0')
		case c >= 'a' && c <= 'f':
			d = int64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int64(c-'A') + 10
		default:
			return 0, fmt.Errorf("bad hex")
		}
		v = v*16 + d
	}
	return v, nil
}

func scanWord(text string) (word, rest string) {
	cut := 0
	for cut < len(text) && !strings.ContainsRune(" \t,]}.:", rune(text[cut])) {
		cut++
	}
	return text[:cut], text[cut:]
}

func isBareRune(r rune) bool {
	return r == '-' || r == '_' ||
		('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}

func digitVal(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
