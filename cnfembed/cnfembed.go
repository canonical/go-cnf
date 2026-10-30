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

// Package cnfembed is a minimal, single-file, streaming
// translator from cnf text to json text. The decoder holds no
// intermediate tree: each value is scanned straight into its
// json fragment, and each block map is an open frame - an
// ordered set of key-value entries - that merges duplicate
// key paths as they arrive and folds into its json object the
// moment its lines end. Only the frames of the maps currently
// open on the indentation stack ever exist.
//
// The grammar, the error messages, and the error positions
// are those of the cnfast parser, kept verbatim - the
// conformance corpus pins them.
package cnfembed

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Options for parsing and rendering. None exist yet,
// here for future compatibility.
type Options struct{}

// ToJSON translates cnf text to json text.
func ToJSON(data []byte, opts *Options) ([]byte, error) {
	if !utf8.Valid(data) {
		return nil, invalidUTF8Err(data)
	}
	p := &parser{lines: splitLines(string(data))}
	return p.parseTop()
}

//
// The document lines and their source positions
//

// A line is one physical line of the document, split into
// its indentation prefix and its content.
type line struct {
	num    int    // 1-based line number, for errors
	indent string // the literal prefix, spaces or tabs
	text   string // content after the prefix
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
		lines[n] = line{num: n + 1, indent: raw[:cut], text: raw[cut:]}
	}
	return lines
}

// A Pos is a source position for errors: 1-based line and
// column.
type pos struct {
	L int
	C int
}

// String returns the position as "L:C".
func (p pos) String() string {
	return fmt.Sprintf("%d:%d", p.L, p.C)
}

func errAt(p pos, msg string) error {
	return fmt.Errorf("%d:%d: %s", p.L, p.C, msg)
}

// errAtScan wraps a scan error at the source position where
// the scanned text begins: pos advanced over the scanned text
// the error's offset names.
func errAtScan(p pos, text string, scanErr error) error {
	serr, ok := scanErr.(scanError)
	if !ok {
		return errAt(p, scanErr.Error())
	}
	offset := serr.offset
	if offset > len(text) {
		offset = len(text)
	}
	delta := text[:offset]
	l := p.L + strings.Count(delta, "\n")
	last := strings.LastIndexByte(delta, '\n')
	c := p.C
	if last >= 0 {
		c = utf8.RuneCountInString(delta[last+1:]) + 1
	} else {
		c += utf8.RuneCountInString(delta)
	}
	return errAt(pos{L: l, C: c}, serr.msg)
}

func indentPos(ln line) pos {
	return pos{L: ln.num, C: len(ln.indent) + 1}
}

// quoteShort quotes a short prefix of s for an error message:
// up to five runes as they stand, then "..." when more follow.
func quoteShort(s string) string {
	const cut = 5
	for i := range s {
		if i == cut {
			s = s[:cut] + "..."
			break
		}
	}
	return fmt.Sprintf("%q", s)
}

// invalidUTF8Err locates the first invalid byte in data and
// names its line and column, both 1-based, counted in runes
// within the line.
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
			return errAt(pos{L: line, C: col}, fmt.Sprintf("cannot decode byte %#x: not UTF-8", data[i]))
		}
		i += size
	}
	// Unreachable: the caller only asks on invalid input.
	return fmt.Errorf("cannot decode document: not UTF-8")
}

//
// The frame: one block map while its lines are read
//

// An entry is one key-value pair of a frame: the key path of
// the line that wrote it (split to a single segment by
// splitKey), and the value - a finished json fragment, or a
// child frame a later line may still merge into.
type entry struct {
	key  []*str
	frag string // the finished json fragment
	sub  *frame // a map value, open for further merges
}

// A frame is one block map in the making: the pairs it holds
// so far, in document order, each key a single segment. The
// keyed insert routes duplicate key paths into the frame a
// matching segment already holds, merging what merges and
// refusing what does not. The frame serializes to its json
// object once, at the end of its lines - after which no line
// can reach it anymore.
type frame struct {
	entries []entry
}

// attach splits the key path of one parsed key line and
// places its value into the frame: the split unwinds from
// the tail - the tail segment becomes the innermost entry's
// key, each segment below it wraps one nesting level - and
// the first segment is inserted under the merge rules.
func attach(m *frame, key []*str, val entry) error {
	if len(key) > 1 {
		val = entry{key: key[len(key)-1:], frag: val.frag, sub: val.sub}
		for j := len(key) - 2; j >= 1; j-- {
			val = entry{key: key[j : j+1], sub: &frame{entries: []entry{val}}}
		}
		val = entry{sub: &frame{entries: []entry{val}}}
	}
	return insert(m, key[:1], val)
}

// insert places one entry into a frame by its key: an
// existing key that prefixes the incoming key routes the
// incoming below it through the existing entry's frame, an
// exact match merges the incoming frame's pairs into the
// existing one, and a key no existing one prefixes appends.
// Both sides of a merge must hold frames; anything else is
// an error, since the json could no longer spell the
// conflict. Keys compare on their decoded values: a.b and
// "a".b are one path.
func insert(m *frame, key []*str, val entry) error {
	for _, held := range m.entries {
		if !prefixes(held.key, key) {
			continue
		}
		if held.sub == nil {
			return fmt.Errorf("%scannot merge with previous %s: not a map", itemPos(key), keyPath(key))
		}
		rest := key[len(held.key):]
		if len(rest) == 0 {
			if val.sub == nil {
				return fmt.Errorf("%scannot merge with previous %s: not a map", itemPos(key), keyPath(key))
			}
			for _, pair := range val.sub.entries {
				if err := insert(held.sub, pair.key, pair); err != nil {
					return err
				}
			}
			return nil
		}
		return insert(held.sub, rest, val)
	}
	m.entries = append(m.entries, entry{key: key, frag: val.frag, sub: val.sub})
	return nil
}

// prefixes reports whether one key path prefixes another,
// compared on their decoded values: a prefixes a.b, and a
// key prefixes itself - the empty remainder is the merge.
func prefixes(a, b []*str) bool {
	if len(a) > len(b) {
		return false
	}
	for i := range a {
		if a[i].Data != b[i].Data {
			return false
		}
	}
	return true
}

// serialize writes the frame as its json object: the pairs
// in document order, each key a single segment written as a
// json string, each value its fragment or its frame in
// turn.
func (f *frame) serialize() string {
	var b strings.Builder
	b.WriteString("{")
	for i, e := range f.entries {
		if i > 0 {
			b.WriteString(",")
		}
		writeJSONString(&b, e.key[0].Data)
		b.WriteString(":")
		if e.sub != nil {
			b.WriteString(e.sub.serialize())
		} else {
			b.WriteString(e.frag)
		}
	}
	b.WriteString("}")
	return b.String()
}

// itemPos writes one key's source position for an error
// message prefix - the line that wrote it, written "L:C: " -
// or the empty string when the start was never located.
func itemPos(key []*str) string {
	if len(key) == 0 {
		return ""
	}
	at := key[0].Pos
	if at.L == 0 && at.C == 0 {
		return ""
	}
	return at.String() + ": "
}

// keyPath spells one key path for an error message: dot-
// joined segments inside backquotes, each in its recorded
// quote style. `a` and `a."b"` are how the forms look.
func keyPath(key []*str) string {
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

//
// The cnf parser, streaming into json fragments
//

type parser struct {
	lines   []line
	pos     int
	pending []line // comment and blank lines, not yet attributed
}

// peekStruct returns the next structural line, collecting the
// blank and comment lines it skips into the pending run. A
// comment shallower than the block's prefix closes the
// block: it is returned as the boundary line, unconsumed.
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

// checkPending validates the pending run at the block's exact
// prefix and drops it: a comment line at any other prefix is
// an error. json cannot carry comments, so the run's content
// itself is dropped - only the grammar's indent rule stands.
func (p *parser) checkPending(prefix string) error {
	err := cmtIndentOK(p.pending, prefix)
	p.pending = nil
	return err
}

func cmtIndentOK(run []line, parent string) error {
	for _, ln := range run {
		if ln.text != "" && ln.indent != parent {
			return errAt(indentPos(ln), "cannot mix indentation prefixes")
		}
	}
	return nil
}

// parseTop reads the file's one value as a finished json
// fragment. The empty file is the empty map, written as a
// block: "{}".
func (p *parser) parseTop() ([]byte, error) {
	first, ok := p.peekStruct("")
	if err := p.checkPending(""); err != nil {
		return nil, err
	}
	if !ok {
		return []byte("{}"), nil
	}
	var frag string
	if looksLikeItem(first.text) {
		// A pair-shaped line opens a map, read to its end as
		// one frame, then serialized.
		m, err := p.parseMap("")
		if err != nil {
			return nil, err
		}
		frag = m.serialize()
	} else {
		// The value opens on its own line - a fence, a list
		// fence, an inline value - and sits at column zero.
		if first.indent != "" {
			return nil, errAt(indentPos(first), "cannot indent a top-level value")
		}
		var err error
		switch {
		case first.text == `"""` || first.text == `'''`:
			frag, err = p.parseFence(first, first.text)
		case first.text == "---":
			frag, err = p.parseListFence(first)
		case isListItem(first.text):
			frag, err = p.parseBlockList(first.indent)
		default:
			// An inline value on the first line, which must end
			// the file.
			p.pos++ // consume the value's own line
			frag, err = p.parseInline(first, first.text)
		}
		if err != nil {
			return nil, err
		}
	}
	if ln, more := p.peekStruct(""); more {
		// The stray structural line at its own start: the
		// content that comes after the top-level value. The
		// message quotes a short prefix of it, so the error
		// shows what the content was.
		return nil, errAt(indentPos(ln), "cannot parse content after top-level value: "+quoteShort(ln.text))
	}
	// The trailing comments the last peek collected ride in
	// the pending run; json has nothing to say about them.
	return []byte(frag), nil
}

// looksLikeItem reports whether text appears to be a
// "path: value" line - a structural check only; the path may
// still be invalid, but parseMap will surface the proper
// error from scanKeyPath, naming the path, not the value
// that follows it.
func looksLikeItem(text string) bool {
	i := strings.IndexByte(text, ':')
	if i <= 0 {
		return false
	}
	c := text[0]
	return c == '"' || c == '\'' || c == '.' || isBareRune(rune(c))
}

// parseMap reads the pairs of one block map at the exact
// prefix into one frame, returned open for its parent to
// merge into.
func (p *parser) parseMap(parentIndent string) (*frame, error) {
	m := &frame{}
	for {
		ln, ok := p.peekStruct(parentIndent)
		if !ok {
			if err := p.checkPending(parentIndent); err != nil {
				return nil, err
			}
			return m, nil
		}
		if ln.indent != parentIndent {
			if len(ln.indent) > len(parentIndent) {
				// The deeper line's own start: the pair that
				// misses the block's prefix is named there.
				return nil, errAt(indentPos(ln), "cannot mix indentation prefixes")
			}
			// A shallower line - structural or comment - closes
			// the block; it stays in the stream for the parent.
			if err := p.checkPending(parentIndent); err != nil {
				return nil, err
			}
			return m, nil
		}
		if err := p.checkPending(parentIndent); err != nil {
			return nil, err
		}
		p.pos++
		key, val, err := p.parseItem(ln)
		if err != nil {
			return nil, err
		}
		if err := attach(m, key, val); err != nil {
			return nil, err
		}
	}
}

// parseItem splits one "path: value" line into its single
// first key segment and its value entry. Positions are
// 1-based: the line's content starts at column
// len(indent)+1, so a token at offset i in text sits at
// column len(indent)+1+i.
func (p *parser) parseItem(ln line) ([]*str, entry, error) {
	base := len(ln.indent) + 1
	path, after, err := scanKeyPath(ln.text)
	if err != nil {
		// The scan started at the line's first text column;
		// the scan error's offset lands on the offender.
		return nil, entry{}, errAtScan(pos{L: ln.num, C: base}, ln.text, err)
	}
	if after == "" || after[0] != ':' {
		// The separator must sit where the scan stopped: the
		// remainder's first column, the honest spot for what
		// refuses to be one.
		return nil, entry{}, errAt(pos{L: ln.num, C: base + len(ln.text) - len(after)}, "cannot find key-value separator ':'")
	}
	for i := range path {
		path[i].Pos.L = ln.num
		path[i].Pos.C = base + path[i].offset
	}
	text := strings.TrimLeft(after[1:], " \t")
	if text == "" {
		next, ok := p.peekStruct(ln.indent)
		if !ok || len(next.indent) <= len(ln.indent) {
			// No value on the line and no deeper block under
			// it: the error names the key line at its own
			// start, and spells the key it holds.
			return nil, entry{}, errAt(indentPos(ln), "cannot omit the value for "+keyPath(path))
		}
		// The block's shape is decided by its first line: a
		// "- " item opens a list, a fence delimiter a fence,
		// a --- line a list fence, anything else a map. The
		// map stays a frame its later siblings may merge
		// into; the other forms close before they return.
		switch {
		case isListItem(next.text):
			frag, err := p.parseBlockList(next.indent)
			return path, entry{frag: frag}, err
		case next.text == `"""` || next.text == `'''`:
			if err := p.cmtNoFencePrefix(); err != nil {
				return nil, entry{}, err
			}
			frag, err := p.parseFence(next, next.text)
			return path, entry{frag: frag}, err
		case next.text == "---":
			if err := p.cmtNoFencePrefix(); err != nil {
				return nil, entry{}, err
			}
			frag, err := p.parseListFence(next)
			return path, entry{frag: frag}, err
		}
		m, err := p.parseMap(next.indent)
		if err != nil {
			return nil, entry{}, err
		}
		return path, entry{sub: m}, nil
	}
	if text[0] == '{' {
		// An inline map in key position stays a frame, not a
		// fragment: a later line's key path may merge into it
		// (a: {b: 1} then a.c: 2).
		pt := pos{L: ln.num, C: len(ln.text) - len(text) + len(ln.indent) + 1}
		m, rest, err := p.scanInlineMapFrame(ln, text, pt)
		if err != nil {
			return nil, entry{}, err
		}
		left, _, ok := stripTrail(rest)
		if !ok && left != "" {
			// The leftover sits at the line's tail: its own first
			// column, the honest spot for what refuses to parse.
			return nil, entry{}, errAt(pos{L: ln.num, C: len(ln.indent) + 1 + len(ln.text) - len(left)}, fmt.Sprintf("cannot parse trailing %q", left))
		}
		return path, entry{sub: m}, nil
	}
	frag, err := p.parseInline(ln, text)
	if err != nil {
		return nil, entry{}, err
	}
	return path, entry{frag: frag}, nil
}

// isListItem reports whether a line's text opens a block
// list item: a "-" followed by a blank or the end of the text.
func isListItem(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "-\t")
}

// parseBlockList reads the "- item" lines of one block list
// at the exact prefix, straight into the json array: each
// element scans to its fragment on the spot, a keyed element
// wrapped in the object its path spells.
func (p *parser) parseBlockList(prefix string) (string, error) {
	var elems []string
	for {
		ln, ok := p.peekStruct(prefix)
		if !ok || ln.indent != prefix || !isListItem(ln.text) {
			if err := p.checkPending(prefix); err != nil {
				return "", err
			}
			return "[" + strings.Join(elems, ",") + "]", nil
		}
		if err := p.checkPending(prefix); err != nil {
			return "", err
		}
		p.pos++
		text := strings.TrimLeft(ln.text[1:], " \t")
		if text == "" {
			// A dash with nothing after it: no key to spell, the
			// item line's own start carries the error.
			return "", errAt(indentPos(ln), "cannot omit the value")
		}
		var key []*str
		// The inlinepair form: a path and a colon before the
		// value; without them the whole text is the value.
		if path, after, kerr := scanKeyPath(text); kerr == nil && after != "" && after[0] == ':' {
			off := len(ln.text) - len(text)
			for i := range path {
				path[i].Pos.L = ln.num
				path[i].Pos.C = len(ln.indent) + 1 + off + path[i].offset
			}
			key = path
			text = strings.TrimLeft(after[1:], " \t")
			if text == "" {
				// The inlinepair form ends on its colon: the key is
				// spelled, the item line's own start carries the
				// error - the same message parseItem raises.
				return "", errAt(indentPos(ln), "cannot omit the value for "+keyPath(path))
			}
		}
		frag, err := p.parseInline(ln, text)
		if err != nil {
			return "", err
		}
		if key != nil {
			frag = wrapFragment(key, frag)
		}
		elems = append(elems, frag)
	}
}

// wrapFragment builds one keyed element's json value: each
// key segment one single-pair object level deep, the value
// innermost - a one-segment key wraps the value directly, a
// multi-segment key nests one object per segment.
func wrapFragment(key []*str, frag string) string {
	for i := len(key) - 1; i >= 0; i-- {
		var b strings.Builder
		b.WriteString("{")
		writeJSONString(&b, key[i].Data)
		b.WriteString(":")
		b.WriteString(frag)
		b.WriteString("}")
		frag = b.String()
	}
	return frag
}

// parseFence reads one fence block from its opening line,
// already peeked as next, into its json string fragment. The
// content is the raw lines between the delimiters at the
// fence's prefix: the closing delimiter closes only at the
// exact prefix, a shallower non-blank line is an error, a
// shallower blank contributes the newline only, and deeper
// lines' excess indent is content. The escaped family
// processes escapes, with a trailing backslash continuing
// the line.
func (p *parser) parseFence(next line, delim string) (string, error) {
	prefix := next.indent
	p.pos++ // consume the opening delimiter
	unfenced := delim == `'''`
	var b strings.Builder
	indentChar := byte(' ')
	if len(prefix) > 0 {
		indentChar = prefix[0]
	}
	continued := false
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		p.pos++
		// The closing delimiter always closes the fence, even
		// when the previous line continued: the trailing escape
		// ate the newline, so the value ends without one.
		if ln.indent == prefix && ln.text == delim {
			return jsonString(b.String()), nil
		}
		if !continued && !sameIndentChar(ln.indent, indentChar) {
			return "", errAt(indentPos(ln), "cannot mix indentation prefixes in fence")
		}
		if len(ln.indent) < len(prefix) {
			if ln.text != "" {
				// A non-empty line at a shallower prefix is a
				// syntax error: the fence closes only at its
				// own exact prefix.
				return "", errAt(indentPos(ln), "cannot close fence at shallower prefix")
			}
			// A blank line with a valid but shallower prefix:
			// the line contributes the newline only.
			b.WriteString("\n")
			continue
		}
		if ln.text == "" {
			// A blank line at or deeper than the fence prefix:
			// its whitespace is content, with its terminator.
			b.WriteString(ln.indent[len(prefix):])
			b.WriteString("\n")
			continue
		}
		if !strings.HasPrefix(ln.indent, prefix) {
			return "", errAt(indentPos(ln), "cannot mix indentation prefixes in fence")
		}
		// The content following the valid prefix is the string,
		// including any excess indentation on deeper lines.
		content := ln.indent[len(prefix):] + ln.text
		contentPos := pos{L: ln.num, C: len(prefix) + 1}
		continued = false
		if !unfenced && oddTrailingBackslashes(content) {
			content = content[:len(content)-1]
			continued = true
		}
		if !unfenced {
			unq, err := unescape(content)
			if err != nil {
				// The content starts at the fence prefix's column;
				// the escape error names the backslash's column,
				// advanced over the content.
				return "", errAtScan(contentPos, content, err)
			}
			content = unq
		}
		b.WriteString(content)
		if !continued {
			b.WriteString("\n")
		}
	}
	return "", errAt(indentPos(next), "cannot find closing fence")
}

// sameIndentChar reports whether the prefix consists solely
// of the given indent character.
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

// unescape processes the closed escape set over raw text.
// Its errors carry the backslash's offset within s, so a
// caller holding the source position can name the exact
// column where the escape starts.
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

// parseListFence reads one --- delimited list from its
// opening line, already peeked as next, into its json array
// fragment. Each element is the lines between two ---
// delimiters at the fence's exact prefix, parsed as a block
// value straight into its fragment.
func (p *parser) parseListFence(next line) (string, error) {
	prefix := next.indent
	p.pos++ // consume the opening ---
	var elems []string
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
			return "", errAt(indentPos(next), "cannot find closing list fence")
		}
		elem := p.lines[start:p.pos]
		if len(elem) == 0 {
			// The empty element opens at the delimiter that
			// closed the previous one - or, for the first
			// element, the fence's own opening line. The error
			// names that line at its own position.
			open := next
			if start > 0 {
				open = p.lines[start-1]
			}
			return "", errAt(indentPos(open), "cannot parse empty list fence element")
		}
		// The indented elements - nested fences - must share
		// one indent: the first one's opening --- sets it, and
		// a later element at a different indent mixes indents
		// inside the fence.
		if elem[0].text == "---" && len(elem[0].indent) > len(prefix) {
			if nested == "" {
				nested = elem[0].indent
			} else if elem[0].indent != nested {
				return "", errAt(indentPos(elem[0]), "cannot mix indents inside list fence")
			}
		}
		p.pos++
		sub := &parser{lines: elem}
		// The element may open with comment and blank lines:
		// the sub-parser's peek skips them, and the dispatch
		// reads the first structural line.
		first, ok := sub.peekStruct(prefix)
		if !ok {
			open := next
			if start > 0 {
				open = p.lines[start-1]
			}
			return "", errAt(indentPos(open), "cannot parse empty list fence element")
		}
		frag, err := sub.parseListFenceValue(first, prefix)
		if err != nil {
			return "", err
		}
		if sub.pos != len(sub.lines) {
			// The leftover line at its own start: the content
			// that comes after the element's value.
			return "", errAt(indentPos(sub.lines[sub.pos]), "cannot parse content in list fence element")
		}
		elems = append(elems, frag)
		// The terminator consumed above ends this element; the
		// fence continues when the next structural line sits at
		// the fence's prefix - the next element's first line -
		// or deeper: a nested list fence element opens at a
		// deeper prefix. A shallower line closes the fence; it
		// stays in the stream for the parent.
		ln, ok := p.peekStruct(prefix)
		if !ok || len(ln.indent) < len(prefix) {
			return "[" + strings.Join(elems, ",") + "]", nil
		}
	}
}

// parseListFenceValue reads one element of a list fence from
// its first line, already peeked, into its json fragment:
// the element is an inline value on its own line, or a
// block - a dashed list, a map, a fence, or a nested list
// fence.
func (p *parser) parseListFenceValue(first line, fence string) (string, error) {
	switch {
	case first.indent == fence:
		switch {
		case isListItem(first.text):
			return p.parseBlockList(first.indent)
		case first.text == `"""` || first.text == `'''`:
			return p.parseFence(first, first.text)
		case looksLikeItem(first.text):
			m, err := p.parseMap(first.indent)
			if err != nil {
				return "", err
			}
			return m.serialize(), nil
		default:
			// An inline value on the element's only line.
			p.pos++ // consume the value's own line
			return p.parseInline(first, first.text)
		}
	case first.text == "---" && len(first.indent) > len(fence):
		// A nested list fence: deeper than the enclosing
		// fence's delimiters.
		return p.parseListFence(first)
	}
	// A dashed list, a map, or an inline item at a deeper
	// prefix: only a nested list fence opens a deeper block
	// inside a fence. The offending element is named at its
	// own position - the line the deeper block opened at.
	return "", errAt(indentPos(first), "list element indent must align with fence")
}

// cmtNoFencePrefix rejects the pending run before a fence
// value: the fence must open at the very next line after
// its pair, so a comment or a blank between them has no
// home - not the fence's content, which is the string, and
// not the pair's line, which is above. The error names the
// offending line at its own position.
func (p *parser) cmtNoFencePrefix() error {
	for _, ln := range p.pending {
		if ln.text != "" {
			return errAt(indentPos(ln), "cannot comment between key and fence")
		}
		return errAt(indentPos(ln), "cannot blank between key and fence")
	}
	return nil
}

// parseInline reads one inline value on the item's line into
// its json fragment. It may carry a trailing comment, which
// the fragment drops.
func (p *parser) parseInline(ln line, text string) (string, error) {
	frag, rest, err := p.scanValueAt(ln, text)
	if err != nil {
		return "", err
	}
	left, _, ok := stripTrail(rest)
	if ok {
		return frag, nil
	}
	if left != "" {
		// The leftover sits at the line's tail: its own first
		// column, the honest spot for what refuses to parse.
		return "", errAt(pos{L: ln.num, C: len(ln.indent) + 1 + len(ln.text) - len(left)}, fmt.Sprintf("cannot parse trailing %q", left))
	}
	return frag, nil
}

// stripTrail splits a trailing comment off the rest of a
// value: at least one blank, then #, then free text to the
// end of the line. The blank is required - a # glued to the
// value is content, not a comment.
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

// scanValueAt reads one inline value from the front of text
// into its json fragment, returning the unread remainder.
func (p *parser) scanValueAt(ln line, text string) (string, string, error) {
	pt := pos{L: ln.num, C: len(ln.text) - len(text) + len(ln.indent) + 1}
	if text == "" {
		// An empty scan: the caller's text is spent - the
		// unclosed list's tail is the usual source. The
		// error names the empty scan's start.
		return "", "", errAt(pt, "cannot parse value at \"\"")
	}
	switch text[0] {
	case '{':
		return p.scanInlineMap(ln, text, pt)
	case '[':
		return p.scanInlineList(ln, text, pt)
	case '"':
		if strings.HasPrefix(text, `"""`) {
			return p.scanFencedLine(ln, text, `"`, pt)
		}
		s, rest, err := scanDoubleQuoted(text)
		if err != nil {
			// The quoted value opens at pt; its escape errors
			// name the backslash's column, advanced over the
			// token.
			return "", "", errAtScan(pt, text, err)
		}
		return jsonString(s), rest, nil
	case '\'':
		if strings.HasPrefix(text, `'''`) {
			return p.scanFencedLine(ln, text, `'`, pt)
		}
		s, rest, err := scanSingleQuoted(text)
		if err != nil {
			// The single-quoted value opens at pt too; its one
			// error - the missing close - names it there.
			return "", "", errAt(pt, err.Error())
		}
		return jsonString(s), rest, nil
	}
	tok, rest := scanNumberToken(text)
	switch tok {
	case "null", "true", "false":
		return tok, rest, nil
	case "":
		// No token at all - the text starts outside every
		// scanned class. The error names the empty token's
		// start, the same anchor the other raises use.
		return "", "", errAt(pt, fmt.Sprintf("cannot parse value at %q", text))
	}
	// A bare word is never a value: the token must be
	// number-shaped to be a number.
	body := strings.TrimPrefix(tok, "-")
	if body == "" || body[0] < '0' || body[0] > '9' {
		return "", "", errAt(pt, fmt.Sprintf("cannot parse unquoted value %q", tok))
	}
	// The base prefixes carry their own digit grammar.
	for _, pfx := range []struct {
		prefix string
		base   int
	}{
		{"0x", 16}, {"0o", 8}, {"0b", 2},
	} {
		if strings.HasPrefix(body, pfx.prefix) {
			digits := body[len(pfx.prefix):]
			if digits == "" {
				return "", "", errAt(pt, fmt.Sprintf("cannot parse number %q", tok))
			}
			for _, c := range digits {
				if v := digitVal(c); v < 0 || v >= pfx.base {
					return "", "", errAt(pt, fmt.Sprintf("cannot parse number %q", tok))
				}
			}
			return numberFragment(tok), rest, nil
		}
	}
	// The grammar is lowercase-only: uppercase exponents are
	// errors, and so are redundant leading zeros.
	if strings.ContainsRune(body, 'E') {
		return "", "", errAt(pt, fmt.Sprintf("cannot parse number %q: uppercase exponent", tok))
	}
	if len(body) > 1 && body[0] == '0' && body[1] >= '0' && body[1] <= '9' {
		return "", "", errAt(pt, fmt.Sprintf("cannot parse number %q: redundant leading zero", tok))
	}
	if !isDecimalShape(body) {
		return "", "", errAt(pt, fmt.Sprintf("cannot parse number %q", tok))
	}
	return tok, rest, nil
}

// isDecimalShape reports whether body matches the grammar's
// decimal spellings: numint [ "." 1*DIGIT ] [ "e" ["+" / "-"]
// 1*DIGIT ]. The redundant leading zero is checked
// separately, for its own message.
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

// scanNumberToken reads one number-shaped token: an optional
// leading minus, digits, letters for the bases, dots, and a
// sign directly after an exponent e.
func scanNumberToken(text string) (tok, rest string) {
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

// scanFencedLine reads the single-line fence forms, """..."""
// and ”'...”', where the escapes stand for the newlines of
// the block form, into the json string fragment of the
// interior they spell. The fence closes at the first
// delimiter; whatever follows is the caller's to read - a
// trailing comment, or an error when it is not one. A stray
// quote after the close, as in ”foo', is a syntax error,
// not content - the grammar is ambiguous there, and the
// typo reading wins. In the escaped family the search skips
// escape sequences, so a \" in the content cannot open a
// false delimiter.
func (p *parser) scanFencedLine(ln line, text, quote string, at pos) (string, string, error) {
	delim := strings.Repeat(quote, 3)
	end := -1
	if quote == `"` {
		unfenced := false
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
		if end < 0 {
			return "", "", errAt(at, "cannot find closing fence")
		}
		inner := text[3 : 3+end]
		innerPos := pos{L: at.L, C: at.C + 3}
		unq, err := unescape(inner)
		if err != nil {
			// The interior starts three quotes past the token
			// start; the escape error names the backslash's
			// column, advanced over the interior.
			_ = unfenced
			return "", "", errAtScan(innerPos, inner, err)
		}
		return jsonString(unq), text[3+end+3:], nil
	}
	end = strings.Index(text[3:], delim)
	if end < 0 {
		return "", "", errAt(at, "cannot find closing fence")
	}
	// The raw family holds the interior as it stands.
	return jsonString(text[3 : 3+end]), text[3+end+3:], nil
}

// scanInlineList reads "[1, 2]" in the one-line form into
// its json array fragment. Elements are inline values,
// recursing through scanValueAt, so nested lists work with
// no extra code.
func (p *parser) scanInlineList(ln line, text string, at pos) (string, string, error) {
	var elems []string
	text = strings.TrimLeft(text[1:], " \t")
	if strings.HasPrefix(text, "]") {
		return "[]", text[1:], nil
	}
	for {
		frag, rest, err := p.scanValueAt(ln, text)
		if err != nil {
			return "", "", err
		}
		elems = append(elems, frag)
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			// The list that never closes is named where it
			// opened: its own start, so nested lists tell apart.
			return "", "", errAt(at, "cannot find closing ']'")
		}
		if rest[0] == ']' {
			return "[" + strings.Join(elems, ",") + "]", rest[1:], nil
		}
		if rest[0] != ',' {
			// The leftover at its own first column: the honest
			// spot for what refuses to be a separator.
			return "", "", errAt(pos{L: ln.num, C: len(ln.indent) + 1 + len(ln.text) - len(rest)}, fmt.Sprintf("cannot parse %q inside list", rest))
		}
		text = strings.TrimLeft(rest[1:], " \t")
	}
}

// scanInlineMap reads "{ a: 1, b: 2 }" in the native item
// grammar into its json object fragment: the frame the
// pairs build, serialized the moment the closing brace
// lands - nothing after the line can reach it anymore.
func (p *parser) scanInlineMap(ln line, text string, at pos) (string, string, error) {
	m, rest, err := p.scanInlineMapFrame(ln, text, at)
	if err != nil {
		return "", "", err
	}
	return m.serialize(), rest, nil
}

// scanInlineMapFrame reads "{ a: 1, b: 2 }" in the native
// item grammar into one frame. The pairs read through the
// same key path splitting and merge rules a block map's
// do.
func (p *parser) scanInlineMapFrame(ln line, text string, at pos) (*frame, string, error) {
	m := &frame{}
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
			return nil, "", errAtScan(pos{L: ln.num, C: len(ln.indent) + 1 + off}, text, err)
		}
		if after == "" || after[0] != ':' {
			// The separator must sit where the scan stopped:
			// the remainder's first column, the honest spot
			// for what refuses to be one.
			return nil, "", errAt(pos{L: ln.num, C: len(ln.indent) + 1 + off + len(text) - len(after)}, "cannot find key-value separator ':'")
		}
		for i := range path {
			path[i].Pos.L = ln.num
			path[i].Pos.C = len(ln.indent) + 1 + off + path[i].offset
		}
		frag, rest, err := p.scanValueAt(ln, strings.TrimLeft(after[1:], " \t"))
		if err != nil {
			return nil, "", err
		}
		if err := attach(m, path, entry{frag: frag}); err != nil {
			return nil, "", err
		}
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			// The map that never closes is named where it
			// opened: its own start, so nested maps tell apart.
			return nil, "", errAt(at, "cannot find closing '}'")
		}
		if rest[0] == '}' {
			return m, rest[1:], nil
		}
		if rest[0] != ',' {
			// The leftover at its own first column: the honest
			// spot for what refuses to be a separator.
			return nil, "", errAt(pos{L: ln.num, C: len(ln.indent) + 1 + len(ln.text) - len(rest)}, fmt.Sprintf("cannot parse %q inside map", rest))
		}
		text = strings.TrimLeft(rest[1:], " \t")
	}
}

//
// The byte scanners, verbatim from the cnfast parser: the
// grammar's tokens, with their errors carrying the offset of
// the offender within the scanned text, so the callers name
// the exact column.
//

// A scanError is an error a byte scanner raises inside the
// text it was scanning.
type scanError struct {
	offset int
	msg    string
}

func (e scanError) Error() string { return e.msg }

// A str is one key segment: its decoded data, its quoting
// style for the error messages that spell a key path, its
// source position, and its offset within the scanned path.
type str struct {
	Data   string
	Quote  quoteKind
	Pos    pos
	offset int
}

// A quoteKind is the quoting style of a key segment.
type quoteKind int

const (
	unquoted     quoteKind = iota // bare word
	doubleQuoted                  // "..."
	singleQuoted                  // '...'
)

// scanKeyPath reads a dotted path of key segments, recording
// each segment's offset within the original text for
// position tracking. Spaces are rejected: between segments,
// after the dot, or before the colon.
func scanKeyPath(text string) ([]*str, string, error) {
	if text == "" || text[0] == '.' {
		return nil, "", scanError{0, "cannot parse empty key segment"}
	}
	var segs []*str
	consumed := 0
	for {
		seg, after, err := scanKey(text)
		if err != nil {
			// A segment scanner error names its spot within the
			// segment; the path advances it by what came before,
			// so the offset lands in the whole path's frame.
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

// scanKey reads one key segment: a bare word or a quoted
// string.
func scanKey(text string) (*str, string, error) {
	if text == "" {
		return nil, "", scanError{0, "cannot parse empty key segment"}
	}
	switch text[0] {
	case '"':
		s, rest, err := scanDoubleQuoted(text)
		if err != nil {
			return nil, "", err
		}
		return &str{Data: s, Quote: doubleQuoted}, rest, nil
	case '\'':
		s, rest, err := scanSingleQuoted(text)
		if err != nil {
			return nil, "", err
		}
		return &str{Data: s, Quote: singleQuoted}, rest, nil
	}
	seg, rest := scanWord(text)
	if seg == "" {
		return nil, "", scanError{0, "cannot parse empty key segment"}
	}
	for _, r := range seg {
		if !isBareRune(r) {
			// A bad word is named at its start: the offset is
			// the segment's own, the caller's consumed count
			// locating it within the path.
			return nil, "", scanError{0, fmt.Sprintf("cannot parse key %q", seg)}
		}
	}
	return &str{Data: seg, Quote: unquoted}, rest, nil
}

// scanDoubleQuoted reads a double-quoted string, processing
// escapes.
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

// scanSingleQuoted reads a single-quoted string: raw, no
// escapes.
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

//
// The json fragments
//

// jsonString writes s as a quoted json string fragment.
func jsonString(s string) string {
	var b strings.Builder
	writeJSONString(&b, s)
	return b.String()
}

// writeJSONString writes s as a json string: the closed set
// escaped, the rest verbatim.
func writeJSONString(b *strings.Builder, s string) {
	b.WriteString(`"`)
	for _, r := range s {
		b.WriteString(escapeJSON(r))
	}
	b.WriteString(`"`)
}

// escapeJSON spells one rune inside a json double-quoted
// string: the closed set escaped, the rest verbatim.
func escapeJSON(r rune) string {
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

// numberFragment writes one number as its json form: the
// decimal forms pass through verbatim - 1.20 keeps its zeros,
// and integers too wide for a float64 keep their precision -
// and the other bases convert to decimal, any width, by
// repeated division of the digit string.
func numberFragment(word string) string {
	switch {
	case strings.HasPrefix(word, "0x"):
		return baseToDecimal(word[2:], 16)
	case strings.HasPrefix(word, "0o"):
		return baseToDecimal(word[2:], 8)
	case strings.HasPrefix(word, "0b"):
		return baseToDecimal(word[2:], 2)
	}
	return word
}

// baseToDecimal converts a digit string in the given base -
// already validated by the scanner - to its decimal form, by
// repeated division by 10: each pass walks the digits once,
// most significant first, carrying the remainder - one
// decimal digit out per pass, least significant first.
func baseToDecimal(digits string, base int) string {
	var out []byte
	for digits != "" {
		carry := 0
		var quot []byte
		for _, c := range digits {
			cur := carry*base + digitVal(c)
			q := cur / 10
			if q < 10 {
				quot = append(quot, '0'+byte(q))
			} else {
				quot = append(quot, 'a'+byte(q-10))
			}
			carry = cur % 10
		}
		// Strip the leading zeros the quotient accumulated; an
		// empty result means the value hit zero and the loop
		// ends.
		i := 0
		for i < len(quot) && quot[i] == '0' {
			i++
		}
		digits = string(quot[i:])
		out = append(out, '0'+byte(carry))
	}
	// The digits came out least significant first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

//
// The cnf quote spelling, kept only for the error messages
// that name a key path: each segment in its recorded family.
//

// Quote returns s quoted and escaped in the q family - one
// of the three families a key segment can hold.
func Quote(s string, q quoteKind) string {
	switch q {
	case unquoted:
		if isBareWord(s) {
			return s
		}
		q = doubleQuoted
		if strings.ContainsRune(s, '"') || strings.ContainsRune(s, '\\') {
			// The single family spells both raw, the cleaner
			// spelling - when it holds the rest of the content.
			q = singleQuoted
			if strings.ContainsRune(s, '\'') || strings.ContainsRune(s, '\n') {
				q = doubleQuoted
			}
		}
	case singleQuoted:
		if strings.ContainsRune(s, '\'') || strings.ContainsRune(s, '\n') {
			q = doubleQuoted
		}
	}
	if q == doubleQuoted {
		var b strings.Builder
		b.WriteRune('"')
		for _, r := range s {
			b.WriteString(escapeCNF(r))
		}
		b.WriteRune('"')
		return b.String()
	}
	if q == singleQuoted {
		return "'" + s + "'"
	}
	return s
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

// escapeCNF spells one rune inside a double-quoted cnf
// string: the closed escape set, \xNN canonical for the C0
// controls.
func escapeCNF(r rune) string {
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
