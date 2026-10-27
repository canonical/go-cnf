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

package cnfpretty

import (
	"fmt"
	"math"
	"strings"

	"github.com/canonical/go-cnf/cnfast"
)

// Options holds the knobs that control reformatting.
//
// The settings evolve with the reformatter, and their exact
// behavior may be fine-tuned across updates.
type Options struct {

	// FoldPathUptoDepth folds single-item map keys onto its
	// parent map key, forming a dotted path. Keys are folded
	// up until reaching the given nesting depth.
	FoldPathUptoDepth int

	// FoldPathFromDepth folds single-item map keys onto its
	// parent map key, forming a dotted path. Keys are folded
	// up starting at the given nesting depth.
	FoldPathFromDepth int

	// UnfoldPathUptoDepth unfolds keys out of dotted paths,
	// forming an expanded single item map. Keys are unfolded
	// until reaching the given nesting depth.
	UnfoldPathUptoDepth int

	// UnfoldPathFromDepth unfolds keys out of dotted paths,
	// forming an expanded single item map. Keys are unfolded
	// starting at the given nesting depth.
	UnfoldPathFromDepth int

	// SoftWrapStringFromWidth wraps a string line that exceeds
	// this width at the next space past the limit, if any.
	// If a hard wrap is set, the space selected is the one
	// closest to the hard limit, if any.
	// The width is of the string line itself, without indents.
	SoftWrapStringFromWidth int

	// HardWrapStringFromWidth wraps a string line that exceeds
	// this width at the exact limit, wherever it falls.
	// The width is of the string line itself, without indents.
	HardWrapStringFromWidth int

	// FoldListUptoItems folds a list into its inline form -
	// one line, comma-separated - while it holds up to and
	// including this many items.
	FoldListUptoItems int

	// UnfoldListFromItems unfolds a list to its block form -
	// one element per line behind a dash - once it holds more
	// than this many items.
	UnfoldListFromItems int

	// FoldMapUptoItems folds a map into its inline form -
	// one line, comma-separated pairs - while it holds up to
	// and including this many items.
	FoldMapUptoItems int

	// UnfoldMapFromItems unfolds a map to its block form -
	// one pair per line - once it holds more than this many
	// items.
	UnfoldMapFromItems int

	// Reindent changes all indents to the provided string.
	Reindent string

	// IgnoreComments allows reformatting to go on even if
	// comments would be lost. The default behavior is to
	// stop reformatting to prevent loss of comments.
	IgnoreComments bool
}

// defaultIndent is the indent the reformat records on a
// container that has none: a map whose children were all
// inline, or a fresh map the reformat builds when it opens a
// value or splits a path. The container's recorded indent,
// when it has one, always wins.
const defaultIndent = "    "

// Pretty is a reformatting profile which is supposed to give
// an AST a reasonable layout. It is aggressive, in that it will
// not attempt to respect choices made in the original AST.
// It is also unstable, in the sense that it evolves with the
// formatter and its behavior may change across updates as the
// subjective idea of what looks good or not is developed.
var _Pretty = Options{}

// unfoldList reports whether the settings rewrite one inline
// list as a block: the list holds this many items or more -
// the threshold included. A list the AST already writes as a
// block is kept; the decision only overrides an inline
// layout.
func (p Options) unfoldList(l *cnfast.List) bool {
	return p.UnfoldListFromItems > 0 && len(l.Items) >= p.UnfoldListFromItems
}

// unfoldMap reports whether the settings rewrite one inline
// map as a block: the same rule, the map's own threshold, the
// map's pairs for the list's items.
func (p Options) unfoldMap(m *cnfast.Map) bool {
	return p.UnfoldMapFromItems > 0 && len(m.Items) >= p.UnfoldMapFromItems
}

// canFoldList reports whether the settings rewrite one block
// or fenced list as inline: the list holds up to and including
// the fold threshold's items, and every item can be written
// inline - a scalar, or a list or map that folds too. A list
// the AST already writes inline is kept; the decision only
// overrides a block or fence layout.
func (p Options) canFoldList(l *cnfast.List) bool {
	if p.FoldListUptoItems <= 0 || len(l.Items) > p.FoldListUptoItems {
		return false
	}
	for i := range l.Items {
		if !p.canInline(l.Items[i]) {
			return false
		}
		// The inline spelling writes elements only: any run an
		// element carries would strand at render, so an element
		// holding one refuses the fold on the list's behalf.
		if !p.IgnoreComments && (p.hasOuterCmts(l.Items[i]) || p.hasInnerCmts(l.Items[i])) {
			return false
		}
	}
	return true
}

// canFoldMap reports whether the settings rewrite one block map
// as inline: the same rule, the map's own threshold, the map's
// pairs for the list's items.
func (p Options) canFoldMap(m *cnfast.Map) bool {
	if p.FoldMapUptoItems <= 0 || len(m.Items) > p.FoldMapUptoItems {
		return false
	}
	for _, it := range m.Items {
		if !p.canInline(it) {
			return false
		}
		// The inline spelling writes pairs only: any run a
		// pair carries would strand at render, so a pair
		// holding one refuses the fold on the map's behalf.
		if p.hasOuterCmts(it) || p.hasInnerCmts(it) {
			return false
		}
	}
	return true
}

// isBlock reports whether an item's value is recorded in
// one of its block forms: a block map, a block list - dashed
// or fenced - or a fence-block string.
func isBlock(v cnfast.Value) bool {
	switch n := v.(type) {
	case *cnfast.Map:
		return n.Block
	case *cnfast.List:
		return n.Block
	case *cnfast.Str:
		return n.Block
	}
	return false
}

// walkBlock reports whether an item's value is a block the
// reformat walks into: a block map or a dashed list, the
// forms whose pairs and items nest under an indent. A
// fenced list keeps its own shape - its elements are its
// content, not nested items - and so does a fenced string.
func walkBlock(v cnfast.Value) bool {
	switch n := v.(type) {
	case *cnfast.Map:
		return n.Block
	case *cnfast.List:
		return n.Block && n.Dashed
	}
	return false
}

func (p Options) canInline(item *cnfast.Item) bool {
	if !isBlock(item.Value) {
		return true
	}
	switch v := item.Value.(type) {
	case *cnfast.Str:
		return v.Quote == cnfast.SingleFenced || v.Quote == cnfast.DoubleFenced
	case *cnfast.List:
		return p.canFoldList(v)
	case *cnfast.Map:
		return p.canFoldMap(v)
	}
	return false
}

// unfoldLayoutPhase opens every inline map and list the
// settings ask for into their block form, and wraps every
// string the settings ask for, one descent over the tree.
// The indent argument is the step the surrounding hierarchy
// uses: a value the walk opens records it, keeping any indent
// it already had, so new levels sit at the tree's own step
// instead of the default.
func (p Options) unfoldLayoutPhase(item *cnfast.Item, indent string) {
	if item == nil {
		return
	}
	if !isBlock(item.Value) {
		switch v := item.Value.(type) {
		case *cnfast.Map:
			if p.unfoldMap(v) {
				v.Block = true
			}
		case *cnfast.List:
			if p.unfoldList(v) {
				v.Block = true
				v.Dashed = true
			}
		case *cnfast.Str:
			p.wrapStr(v)
		}
		if isBlock(item.Value) && item.Indent == "" {
			if indent == "" {
				indent = defaultIndent
			}
			item.Indent = indent
		}
	}
	switch v := item.Value.(type) {
	case *cnfast.Map:
		if v.Block {
			if s := observedIndent(v); s != "" {
				indent = s
			}
			for _, it := range v.Items {
				p.unfoldLayoutPhase(it, indent)
			}
		}
	case *cnfast.List:
		if v.Block && v.Dashed {
			for i := range v.Items {
				el := v.Items[i]
				p.unfoldLayoutPhase(el, indent)
				// The unfold may leave an element holding a
				// block, which a dash line cannot spell: a
				// chain of single pairs folds back onto the
				// dash, the rest demotes to the fence at
				// render.
				if pk, pv, ok := p.foldNestedPath(el.Value); ok {
					el.Key = append(el.Key, pk...)
					el.Value = pv
				}
			}
		}
	}
}

// observedIndent returns the indent one map's level uses:
// the first pair holding one. Empty means none did, and the
// caller falls back to what it carried or the default.
func observedIndent(m *cnfast.Map) string {
	for _, it := range m.Items {
		if it.Indent != "" {
			return it.Indent
		}
	}
	return ""
}

// foldNestedPath folds a chain of single-pair block maps
// onto the key path it returns, the innermost non-chain value
// alongside it. It reports ok=false when the value holds no
// such chain. The caller uses it to keep a dash line's
// element spellable once the unfold has opened the maps
// under it: a block on a dash line cannot be spelled, so the
// chain folds onto the element's key instead.
func (p Options) foldNestedPath(v cnfast.Value) (key []*cnfast.Str, val cnfast.Value, ok bool) {
	m, isMap := v.(*cnfast.Map)
	if !isMap || !m.Block || len(m.Items) != 1 {
		return nil, v, false
	}
	pair := m.Items[0]
	if !isBlock(pair.Value) {
		return pair.Key, pair.Value, true
	}
	if p.hasOuterCmts(pair) || p.hasInnerCmts(pair) {
		return nil, v, false
	}
	if k2, v2, ok2 := p.foldNestedPath(pair.Value); ok2 {
		return append(pair.Key, k2...), v2, true
	}
	return nil, v, false
}

// wrapStr breaks the lines of one string that are longer than
// the wrap width, records the result as the string's Text, and
// switches it to the fence form. The decoded value in Str is not
// touched. It returns whether any change was made; a string whose
// every line fits the width is left alone entirely, and so is a
// string that is over the width but has no place to break.
func (p Options) wrapStr(s *cnfast.Str) bool {
	trip := p.SoftWrapStringFromWidth
	if p.HardWrapStringFromWidth > 0 {
		trip = p.HardWrapStringFromWidth
	}
	if trip <= 0 || len(s.Data) <= trip {
		// Either the knobs are off, or even the whole string
		// fits on one line - no line can be longer than
		// that, so the split would only be wasted work.
		return false
	}
	newlineEnd := strings.HasSuffix(s.Data, "\n")
	lines := strings.Split(s.Data, "\n")
	if newlineEnd {
		lines = lines[:len(lines)-1] // the final newline is the last line's tail
	}
	over := false
	for _, line := range lines {
		if len(line) > trip {
			over = true
			break
		}
	}
	if !over {
		return false
	}
	text := make([]string, 0, len(lines)+len(s.Data)/trip)
	for i, line := range lines {
		for len(line) > trip {
			cut := p.wrapLineIndex(line)
			if cut <= 0 {
				break // no break point: the piece is kept, over the width
			}
			text = append(text, wrapEncodeLine(line[:cut], false))
			line = line[cut:]
		}
		last := i == len(lines)-1
		text = append(text, wrapEncodeLine(line, !last || newlineEnd))
	}
	s.Text = text
	s.Block = true
	if s.Quote != cnfast.SingleFenced && s.Quote != cnfast.DoubleFenced {
		s.Quote = cnfast.DoubleFenced
	}
	return true
}

// wrapEncodeLine spells one wrapped piece as a record element:
// the piece through cnfast.Quote, the compact delimiters
// stripped, the tail appended - the marker when the piece
// continues, the terminator otherwise.
func wrapEncodeLine(piece string, terminated bool) string {
	quoted := cnfast.Quote(piece, cnfast.DoubleFenced)
	body := strings.TrimSuffix(strings.TrimPrefix(quoted, `"""`), `"""`)
	if terminated {
		return body + "\n"
	}
	return body + `\`
}

// wrapLineIndex returns where one over-width line should
// break next. With a hard limit set, the last space between
// the soft floor and the hard limit wins, so the break lands
// as late as the window allows and the word stays whole; if
// there is no space in that window, the break falls exactly
// at the hard limit, mid-word. With no hard limit, the first
// space past the soft floor wins - the earliest break is the
// useful one - and if there is no space at all, the line
// cannot break and is kept as is (a zero return). The space
// at the break, when there is one, stays on the first piece:
// the returned index points just past it, so the backslash
// that continues the line lands after the space.
func (p Options) wrapLineIndex(line string) int {
	from := p.SoftWrapStringFromWidth
	if from < 1 {
		from = 1
	}
	to := len(line)
	if p.HardWrapStringFromWidth > 0 && p.HardWrapStringFromWidth < to {
		to = p.HardWrapStringFromWidth
	}
	if from > to {
		from = to
	}
	cut := -1
	if p.HardWrapStringFromWidth > 0 {
		// The window: the last space between the soft floor
		// and the hard deadline wins - the break as late as
		// the window allows.
		if idx := strings.LastIndexByte(line[from:to], ' '); idx >= 0 {
			cut = from + idx
		}
	} else if idx := strings.IndexByte(line[from:], ' '); idx >= 0 {
		// Soft alone: the first space past the floor wins -
		// no deadline to defer for, the earliest break the
		// useful one.
		cut = from + idx
	}
	if cut < 0 {
		if p.HardWrapStringFromWidth <= 0 {
			return 0 // soft alone, no space: the line cannot break
		}
		cut = p.HardWrapStringFromWidth
	}
	if cut < len(line) && line[cut] == ' ' {
		return cut + 1 // the space stays on the broken piece
	}
	return cut
}

// pathRange resolves the depth range a folded key line may
// span, the near side lo and the far side hi: [1, n] under
// the upto knob alone - the top through the budget,
// inclusive - [n, inf) under the from knob alone - the
// threshold itself included, both sides inclusive - and
// their intersection under both, empty when the two miss: a
// line rooted before the near side or ending past the far
// side is kept. Both unset, the range is empty: no path
// folds.
func (p Options) pathRange() (lo, hi int) {
	if p.FoldPathUptoDepth <= 0 && p.FoldPathFromDepth <= 0 {
		return 0, 0 // no path fold
	}
	lo, hi = 1, math.MaxInt
	if p.FoldPathFromDepth > 0 {
		lo = p.FoldPathFromDepth
	}
	if p.FoldPathUptoDepth > 0 {
		hi = p.FoldPathUptoDepth
	}
	return lo, hi
}

// insert places one item into a map by its key: an existing
// key that prefixes the incoming key routes the incoming
// below it through the existing item's value map, an exact
// match merges the incoming items into the existing map, and
// a key no existing one prefixes appends. Both sides of a
// merge must hold block maps; anything else is an error,
// since the tree could no longer be spelled. Keys compare on
// their decoded values: a.b and "a".b are one path.
func insert(m *cnfast.Map, item *cnfast.Item) error {
	for _, held := range m.Items {
		if !prefixes(held.Key, item.Key) {
			continue
		}
		heldMap, ok := held.Value.(*cnfast.Map)
		if !ok || !heldMap.Block {
			return pathErrorf("%scannot merge with previous %s: not a map", itemPos(item), item.Key)
		}
		rest := item.Key[len(held.Key):]
		if len(rest) == 0 {
			itemMap, ok := item.Value.(*cnfast.Map)
			if !ok || !itemMap.Block {
				return pathErrorf("%scannot merge with previous %s: not a map", itemPos(item), item.Key)
			}
			for _, it := range itemMap.Items {
				if err := insert(heldMap, it); err != nil {
					return err
				}
			}
			return nil
		}
		inner := &cnfast.Item{
			Key:     rest,
			Value:   item.Value,
			PreCmt:  item.PreCmt,
			HeadCmt: item.HeadCmt,
			FootCmt: item.FootCmt,
			InCmt:   item.InCmt,
			PostCmt: item.PostCmt,
		}
		return insert(heldMap, inner)
	}
	m.Items = append(m.Items, item)
	// A block value needs an indent to place its content;
	// one that never got one - a programmatically built item -
	// records the default here.
	if isBlock(item.Value) && item.Indent == "" {
		item.Indent = defaultIndent
	}
	return nil
}

// prefixes reports whether one key path prefixes another,
// compared on their decoded values: a prefixes a.b, and a
// key prefixes itself - the empty remainder is the merge.
func prefixes(a, b []*cnfast.Str) bool {
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

// path writes one key path for an error message, inside
// backquotes, each segment in its recorded quote style -
// the same form the render writes - so the path never
// blends into the phrase around it. `a` and `a."b"` are
// how the forms look.
func path(key []*cnfast.Str) (string, error) {
	var b strings.Builder
	b.WriteString("`")
	for i, k := range key {
		if i > 0 {
			b.WriteString(".")
		}
		b.WriteString(cnfast.Quote(k.Data, k.Quote))
	}
	b.WriteString("`")
	return b.String(), nil
}

// itemPos writes one item's source position for an error
// message prefix: the position of its key's first segment -
// the line that wrote the item - written "L:C: ", or the
// empty string when the position is zero or the item carries
// no key - a programmatically built AST holds no source, and
// a message it prefixes writes nothing.
func itemPos(item *cnfast.Item) string {
	if len(item.Key) == 0 {
		return ""
	}
	pos := item.Key[0].Pos
	if pos.L == 0 && pos.C == 0 {
		return ""
	}
	return pos.String() + ": "
}

// pathErrorf formats one error message, every key path
// argument - a []*cnfast.Str, the type of item.Key - written as its
// formatted path before the format runs: the paths render
// inside backquotes, never blending into the phrase. A path
// that cannot be written is itself the error, returned in place
// of the message; otherwise the written args pass to
// Errorf, which does the actual job.
func pathErrorf(format string, args ...any) error {
	for i, arg := range args {
		key, ok := arg.([]*cnfast.Str)
		if !ok {
			continue
		}
		spelled, err := path(key)
		if err != nil {
			return err
		}
		args[i] = spelled
	}
	// The path has the wrong type according to go vet. We
	// can workaround this later by having an actual String
	// method there, but we still want the logic here because
	// this handles errors more appropriately.
	f := format
	return fmt.Errorf(f, args...)
}

func (p Options) hasOuterCmts(item *cnfast.Item) bool {
	if p.IgnoreComments {
		return false
	}
	return len(item.PreCmt.Text) > 0 || len(item.PostCmt.Text) > 0 || len(item.InCmt.Text) > 0
}

func (p Options) hasInnerCmts(item *cnfast.Item) bool {
	if p.IgnoreComments {
		return false
	}
	return len(item.HeadCmt.Text) > 0 || len(item.FootCmt.Text) > 0
}

// foldPathItem folds one item's key line into the output map,
// at the depth the line's first segment sits, under an
// enclosing key prefix: the single-item levels below the
// item join its key while the grown line stays within the
// fold's depth range, the value staying on the folded line
// verbatim; a multi-item level dissolves, its items folding
// under the grown key and inserting in the item's place; a
// line outside the range is kept as recorded, a committed
// prefix nesting the item under the line so far - the fold
// never splits a dotted segment.
func (p Options) foldPathItem(out *cnfast.Map, item *cnfast.Item, prefix []*cnfast.Str, root int) error {
	lo, hi := p.pathRange()
	// Pre/Post comments on item prevent the fold to avoid loss.
	outerCmts := p.hasOuterCmts(item)
	key := append(append([]*cnfast.Str{}, prefix...), item.Key...)
	for {
		if end := root + len(key) - 1; end < lo || end > hi || outerCmts {
			// The line does not fold: its key would span depths outside the range,
			// or the item has comments that would be lost on folding,
			// so the key being built is dropped and the inserted item keeps the
			// prefix while the latest key remains in a map value inside it.
			if len(prefix) > 0 {
				// A prefix was defined by the recursive calls, inject an item with it.
				item = &cnfast.Item{
					Key:    append([]*cnfast.Str{}, prefix...),
					Value:  &cnfast.Map{Items: []*cnfast.Item{item}, Block: true},
					Indent: defaultIndent,
				}
			}
			break
		}
		inner, ok := item.Value.(*cnfast.Map)
		if !ok || !inner.Block {
			// A scalar or inline value: the fold ends here, the
			// value staying verbatim on the folded key.
			item.Key = key
			break
		}
		if p.hasInnerCmts(item) {
			// Head/Foot comments prevent folding past item to avoid loss.
			item.Key = key
			break
		}
		if len(inner.Items) != 1 {
			// A multi-item map: the item dissolves, each inner
			// item folding under the grown key as its prefix.
			return p.foldPathInto(out, inner, key, root)
		}
		// A single-item block map: absorb the level's key
		// segments into the key, the level's item becoming the
		// item, and loop.
		single := inner.Items[0]
		if root+len(key)+len(single.Key)-1 > hi {
			// Absorbing the level would end the line past the
			// far side: the fold ends, the level nesting under
			// the key. The out map's indent places the level's
			// block under the folded key's line.
			item.Key = key
			break
		}
		if m, ok := single.Value.(*cnfast.Map); ok && m.Block && p.hasOuterCmts(single) {
			// Going past this level would mean its comments would be lost.
			item.Key = key
			break
		}
		key = append(key, single.Key...)
		item.Value = single.Value
		item.PreCmt = single.PreCmt
		item.PostCmt = single.PostCmt
		item.HeadCmt = single.HeadCmt
		item.FootCmt = single.FootCmt
		item.InCmt = single.InCmt
	}
	return insert(out, item)
}

// foldPathInto folds one map's items into the output map, under
// an enclosing key prefix, the items' first key segments at
// the given depth.
func (p Options) foldPathInto(out, m *cnfast.Map, prefix []*cnfast.Str, root int) error {
	for _, item := range m.Items {
		if err := p.foldPathItem(out, item, prefix, root); err != nil {
			return err
		}
	}
	return nil
}

// foldPaths folds the paths of one map's items, their first
// key segments at the given depth, under an enclosing key
// prefix: each item's nesting levels join its key, the value
// staying on the folded line verbatim, a line outside the
// fold's depth range kept or nesting its remainder. The
// map's item list is rebuilt by the keyed insert: a key the
// map already holds merges, so the duplicates a fold would
// otherwise emit never exist.
func (p Options) foldPaths(m *cnfast.Map, prefix []*cnfast.Str, root int) error {
	lo, _ := p.pathRange()
	if lo <= 0 {
		return nil
	}
	out := &cnfast.Map{Items: make([]*cnfast.Item, 0, len(m.Items)), Pos: m.Pos}
	if err := p.foldPathInto(out, m, prefix, root); err != nil {
		return err
	}
	m.Items = out.Items
	return nil
}

// unfoldPathsPhase splits every key line within the knobs'
// limits, one descent over the tree: each block map's item
// list is rebuilt by the keyed insert - the split chains
// inserting, a key the map already holds merging - and the
// walk recurses into the rebuilt items' values, the split's
// fresh chains included. An inline map or a fence is a
// value whose layout is kept. Each map's placement derives
// from the indent the map's enclosing container records.
func (p Options) unfoldPathsPhase(item *cnfast.Item, root int) error {
	if item == nil {
		return nil
	}
	switch v := item.Value.(type) {
	case *cnfast.Map:
		if v.Block {
			if err := p.unfoldPaths(v, root); err != nil {
				return err
			}
			for _, it := range v.Items {
				if err := p.unfoldPathsPhase(it, root+len(it.Key)); err != nil {
					return err
				}
			}
		}
	case *cnfast.List:
		if v.Block && v.Dashed {
			for i := range v.Items {
				if err := p.unfoldPathsPhase(v.Items[i], root); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// unfoldPaths unfolds the paths of one map's items, their
// first key segments at the given depth: each item's dotted
// segments within the knobs' limits split off its key, one
// nesting level per segment, the value staying on the
// innermost unfolded key's line verbatim. The map's item
// list is rebuilt by the keyed insert: a key the map
// already holds merges, so the duplicates an unfold would
// otherwise emit never exist.
func (p Options) unfoldPaths(m *cnfast.Map, root int) error {
	if p.UnfoldPathFromDepth <= 0 && p.UnfoldPathUptoDepth <= 0 {
		return nil
	}
	out := &cnfast.Map{Items: make([]*cnfast.Item, 0, len(m.Items)), Pos: m.Pos}
	for _, item := range m.Items {
		if err := p.unfoldItem(out, item, root); err != nil {
			return err
		}
	}
	m.Items = out.Items
	return nil
}

// unfoldItem unfolds one item's key line into the output
// map, at the depth the line's first segment sits: the
// segments within the knobs' limits split off the key, one
// nesting level per segment, the value staying on the
// innermost unfolded key's line verbatim - an inline map is
// a value, the unfold never opens it. Each fresh level is
// one default indent under its parent: the fresh maps carry
// the indent themselves, since the AST recorded none for
// them. The segments before the range stay dotted on the
// outer key - at least the first, the start kept - the
// ones past it staying dotted on the innermost. A line
// holding no segment within the limits is kept whole.
func (p Options) unfoldItem(out *cnfast.Map, item *cnfast.Item, root int) error {
	key := item.Key
	// The splitting range: the segments at depths within the
	// knobs' limits - [from, inf) under the from knob, the
	// deep side, [root, upto] under the upto knob, the
	// shallow side - both inclusive, their intersection under
	// both. The from limit keeps the boundary: the segments
	// at depths up to and including it stay on the key - the
	// fold's upto boundary, mirrored - the ones past it
	// splitting, one nesting level each.
	s := 0
	if p.UnfoldPathFromDepth > 0 {
		if n := p.UnfoldPathFromDepth - root + 1; n > s {
			s = n
		}
	}
	e := len(key)
	if p.UnfoldPathUptoDepth > 0 {
		if n := p.UnfoldPathUptoDepth - root + 1; n < e {
			e = n
		}
	}
	// The chain: the outer key o segments, the levels between
	// o and i, the innermost from i - the tail when the range
	// ends before the key's end, the last splitting segment
	// when it ends at it, the value staying on the innermost.
	o := s
	if o < 1 {
		o = 1 // the first splitting segment: the outer key
	}
	i := e
	if i == len(key) {
		i-- // no tail: the last splitting segment becomes the innermost key
	}
	if e <= s || i < o {
		// No segment within the limits, or nothing left to
		// stay: the line is kept whole - the phase's descent
		// splits its value's items underneath.
		return insert(out, item)
	}
	// The line splits: the segments before the range stay
	// dotted on the outer key, each segment within it opening
	// one nesting level, the segments past it staying dotted
	// on the innermost key, the value staying there. The
	// innermost item carries the value with its recorded
	// layout; each fresh item of the chain records the
	// default indent, so the nesting levels sit one default
	// indent apart, with the AST recording none for them.
	item.Key = key[:o]
	inner := &cnfast.Item{
		Key:     key[i:],
		Value:   item.Value,
		HeadCmt: item.HeadCmt,
		FootCmt: item.FootCmt,
		InCmt:   item.InCmt,
		Indent:  defaultIndent,
	}
	if !isBlock(inner.Value) {
		inner.Indent = "" // an inline or scalar value carries no fresh level
	}
	level := &cnfast.Map{Items: []*cnfast.Item{inner}, Block: true}
	for j := i - 1; j >= o; j-- {
		inner = &cnfast.Item{
			Key:    []*cnfast.Str{key[j]},
			Value:  level,
			Indent: defaultIndent,
		}
		level = &cnfast.Map{Items: []*cnfast.Item{inner}, Block: true}
	}
	item.Value = level
	item.Indent = defaultIndent
	return insert(out, item)
}

// foldPathsPhase folds every key line within the fold's
// depth range, one descent over the tree: each block map's
// item list is rebuilt by the keyed insert - the folded
// lines inserting, a key the map already holds merging -
// and the walk recurses into the rebuilt items' values.
// An inline map or a fence is a value whose layout is kept.
func (p Options) foldPathsPhase(item *cnfast.Item, root int) error {
	if item == nil {
		return nil
	}
	switch v := item.Value.(type) {
	case *cnfast.Map:
		if v.Block {
			if err := p.foldPaths(v, nil, root); err != nil {
				return err
			}
			for _, it := range v.Items {
				if err := p.foldPathsPhase(it, root+len(it.Key)); err != nil {
					return err
				}
			}
		}
	case *cnfast.List:
		if v.Block && v.Dashed {
			for i := range v.Items {
				if err := p.foldPathsPhase(v.Items[i], root); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// foldLayoutPhase folds the layouts, bottom-up, one descent
// over the tree: the children decide first - a block value
// holding up to the threshold's items, everything under it
// folding too, closes into its inline form - then the
// item's own layout folds. An inline map or a fence is a
// value whose layout is kept.
func (p Options) foldLayoutPhase(item *cnfast.Item) {
	if item == nil {
		return
	}
	switch v := item.Value.(type) {
	case *cnfast.Map:
		if v.Block {
			for _, it := range v.Items {
				p.foldLayoutPhase(it)
			}
		}
	case *cnfast.List:
		// A fence's elements still fold: the fence is the
		// list's form, the elements are items like any
		// other, their own counts deciding.
		if v.Block {
			for i := range v.Items {
				p.foldLayoutPhase(v.Items[i])
			}
		}
	}
	p.foldLayout(item)
}

// foldLayout closes one item's block value into its inline
// form when the settings ask - a block or fenced map or
// list holding up to the threshold's items, everything under
// it folding too, the phase having decided the children
// first. The folded items are written inline with it, a
// keyed one wrapped as its single-pair map first - the
// inline spelling of a dash line's key.
func (p Options) foldLayout(item *cnfast.Item) {
	switch v := item.Value.(type) {
	case *cnfast.Map:
		if v.Block && p.canFoldMap(v) {
			v.Block = false
			for _, it := range v.Items {
				if isBlock(it.Value) {
					setInline(it)
				}
			}
		}
	case *cnfast.List:
		if v.Block && p.canFoldList(v) {
			v.Block = false
			v.Dashed = false
			for i := range v.Items {
				if len(v.Items[i].Key) > 0 {
					// A keyed element joins the inline list as
					// its single-pair map: the dash spelling
					// has no inline form of its own.
					wrapKeyed(v.Items[i])
				}
				if isBlock(v.Items[i].Value) {
					setInline(v.Items[i])
				}
			}
		}
	}
}

// wrapKeyed moves one keyed item's key onto its value: the
// value becomes the single-pair map holding the key, the
// item's own key cleared - the inline spelling of a keyed
// element, which a dash line spells as "- key: value" and an
// inline list holds as "{key: value}". The two are one value
// in two spellings; the wrap is the re-spelling, the value
// untouched. A multi-segment key rides the wrapper whole -
// the inline pair keeps its dots.
func wrapKeyed(item *cnfast.Item) {
	if len(item.Key) == 0 {
		return
	}
	item.Value = &cnfast.Map{
		Items: []*cnfast.Item{{Key: cloneStrs(item.Key), Value: item.Value, InCmt: item.InCmt}},
		Block: false,
	}
	item.Key = nil
	item.InCmt = cnfast.Cmt{}
}

// cloneStrs deep-copies one key slice, a fresh slice sharing
// no Str with the original.
func cloneStrs(key []*cnfast.Str) []*cnfast.Str {
	out := make([]*cnfast.Str, len(key))
	for i, s := range key {
		s2 := *s
		out[i] = &s2
	}
	return out
}

// setInline marks one item's value as inline: the block
// flags cleared, the indent dropped. A folded map or list
// writes its items inline with it, whatever they held.
func setInline(item *cnfast.Item) {
	switch v := item.Value.(type) {
	case *cnfast.Map:
		v.Block = false
	case *cnfast.List:
		v.Block = false
		v.Dashed = false
	case *cnfast.Str:
		v.Block = false
	}
	item.Indent = ""
}

// Apply reformats the item hierarchy according to the
// provided options.
func Apply(item *cnfast.Item, opts *Options) error {
	if opts == nil {
		opts = &Options{}
	}
	return opts.apply(item)
}

// reindentPhase rewrites every block level's indent to
// the setting, one walk over the tree. A block-valued item
// holding an empty indent keeps it: after the phases, the
// only such item is a fence element whose block sits at
// the fence's own base - the honest zero distance - and
// the swap must not move it.
func (p Options) reindentPhase(item *cnfast.Item) {
	if item == nil {
		return
	}
	if isBlock(item.Value) && item.Indent != "" {
		item.Indent = p.Reindent
	}
	switch v := item.Value.(type) {
	case *cnfast.Map:
		if v.Block {
			for _, it := range v.Items {
				p.reindentPhase(it)
			}
		}
	case *cnfast.List:
		if v.Block {
			for i := range v.Items {
				p.reindentPhase(v.Items[i])
			}
		}
	}
}

// apply runs the reformat over one whole item tree, the
// Options carrying the settings.
func (p Options) apply(item *cnfast.Item) error {
	p.unfoldLayoutPhase(item, "")
	if err := p.unfoldPathsPhase(item, 1); err != nil {
		return err
	}
	if err := p.foldPathsPhase(item, 1); err != nil {
		return err
	}
	p.foldLayoutPhase(item)
	if p.Reindent != "" {
		p.reindentPhase(item)
	}
	return nil
}
