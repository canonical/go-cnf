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

// Do not import "encoding/json" in here. The ast package is not supposed
// to force the linking of the entire json package.
import (
	_ "os"
	"strconv"
)

// Each AST type names its fields for what they hold: the
// string's decoded text, the number's written spelling, the
// containers' items. The JSON tags are shorter, to keep the
// JSON form compact and easy to read on inspection.

// A Value is any node that can sit in a value position: the value
// of a key, or an element of a list. The unexported marker method
// keeps construction inside the package, so an AST can never hold
// a stray type, and it documents the value grammar in one place.
type Value interface {
	value() // marker
}

// A Pos is a source position: 1-based line and column.
type Pos struct {
	L int `json:"l"`
	C int `json:"c"`
}

// String spells the position as "L:C": the 1-based line
// and column, for an error message.
func (p Pos) String() string {
	return strconv.Itoa(p.L) + ":" + strconv.Itoa(p.C)
}

// A Str is a string: the decoded text, with the quote style of
// its spelling. Unquoted is keys-only: a bare word is never a
// value. Block records the two states a Str can take: unset
// for the single-line spellings - quoted and the one-line
// fence - set for the fence block spelling, the fence on its
// own lines, indented under its key. A quoted string can
// never be spelled as a block, so a set Block is only read as
// a fence block when Quote holds one of the fenced families.
//
// Text holds the encoded lines of the recorded spelling, one
// per source line, with the indentation stripped. When Text
// is set, the render writes it as is, after checking that it
// still decodes to Data; when it is not, the render encodes
// Data from scratch. If Text decodes to something else, the
// two fields disagree - someone changed Data without calling
// Set - and the render fails instead of guessing which one
// is current.
type Str struct {
	Data  string    `json:"str"`
	Text  []string  `json:"-"` // the encoded lines, unset when spelled fresh
	Pos   Pos       `json:"pos"`
	Quote QuoteKind `json:"quote,omitzero"`
	Block bool      `json:"block,omitzero"`

	offset int `json:"-"` // within the line's text, for Pos
}

func (*Str) value() {}

// Set stores a new decoded value and clears Text, so the next
// render encodes the new value from scratch instead of
// writing the old recorded spelling.
func (s *Str) Set(str string) {
	s.Data = str
	s.Text = nil
}

// A QuoteKind is the quoting style of a string.
type QuoteKind int

const (
	Unquoted     QuoteKind = iota // bare word, keys only
	SingleQuoted                  // '...'
	DoubleQuoted                  // "..."
	SingleFenced                  // '''...'''
	DoubleFenced                  // """..."""
)

// A Num is a number, kept exactly as it was written.
type Num struct {
	Text string `json:"num"`
	Pos  Pos    `json:"pos"`
}

func (*Num) value() {}

// A Scalar is a bare keyword: "null", "true", "false".
type Scalar struct {
	Text string `json:"scalar"`
	Pos  Pos    `json:"pos"`
}

func (*Scalar) value() {}

// A Cmt is a run of comment lines, one element per line,
// copied verbatim from the source, indent included. A blank
// line is the empty string. The elements are slices of the
// parsed text, so building a run allocates only the slice.
// Pos points at the run's first line: the blank divider line
// before a pre or post run, or the first blank after the
// value for an in comment.
type Cmt struct {
	Text []string `json:"cmt"`
	Pos  Pos      `json:"pos"`
}

// An Item is one element of a document: a map pair (one
// "key: value" line), a list element, or the root value. The
// key is absent when the element carries no path. The
// comment runs around it sit in source order: PreCmt holds
// the comments before the line, HeadCmt the comments inside
// the block value before its first item, InCmt the comments
// right after the value, FootCmt the comments inside the
// block value after its last item, and PostCmt the comments
// after the line, up to the first blank. The blank divider
// between two runs is not stored anywhere; the render
// writes it back where the source had it.
//
// Indent is the indent added for the content under this
// item. A block value held by the item opens at the item's
// line prefix plus this indent, an empty string adding
// nothing. The empty string means zero distance, it is not
// a status flag. Two cases hold it by design. A fence
// element's flat content: the element has no indent of its
// own, its base is the fence's, so the distance is zero.
// An inline value: it can also hold an indent handed down
// from the tree, so a later unfold opens it at the tree's
// own step. The grammar, not this field, forces a non-zero
// indent where it does: a block under a pair must open
// strictly deeper than the pair.
type Item struct {
	Key     []*Str `json:"key,omitempty"`
	PreCmt  Cmt    `json:"precmt,omitzero"`
	HeadCmt Cmt    `json:"headcmt,omitzero"`
	Value   Value  `json:"value"`
	InCmt   Cmt    `json:"incmt,omitzero"`
	FootCmt Cmt    `json:"footcmt,omitzero"`
	PostCmt Cmt    `json:"postcmt,omitzero"`
	Indent  string `json:"indent,omitzero"`
}

// A List is a list of items. Block defines if the list is
// inline or not, and Dashed defines if the Block form is
// expressed as a compact list of dashed items, or otherwise
// the fence form that allows block children.
type List struct {
	Items  []*Item `json:"list"`
	Dashed bool    `json:"dashed,omitzero"`
	Block  bool    `json:"block,omitzero"`
	Pos    Pos     `json:"pos"`
}

func (*List) value() {}

// A Map is a block of pairs. Block defines if the map is
// inline or indented under its parent. A map has no fenced
// spelling, so the block form is the only other one. The
// comment runs around its pairs live on the enclosing
// item: its HeadCmt and FootCmt.
type Map struct {
	Items []*Item `json:"map"`
	Block bool    `json:"block,omitzero"`
	Pos   Pos     `json:"pos"`
}

func (*Map) value() {}
