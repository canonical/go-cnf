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

// A Value is any node that can sit in a value position: Str, Num, Scalar, Map, List.
type Value interface {
	value() // marker
}

// A Pos is a source position: 1-based line and column.
type Pos struct {
	L int `json:"l"`
	C int `json:"c"`
}

// String returns the position as "L:C": the 1-based line and column.
func (p Pos) String() string {
	return strconv.Itoa(p.L) + ":" + strconv.Itoa(p.C)
}

// A Str represents a string in the document. Unquoted (bare)
// strings can only be used in key positions, but they are
// accepted by the renderer and will escalate to the best
// guess of a reasonable representation.
type Str struct {
	Data  string    `json:"str"`
	Text  []string  `json:"-"`
	Pos   Pos       `json:"pos"`
	Quote QuoteKind `json:"quote,omitzero"`
	Block bool      `json:"block,omitzero"`

	offset int
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

// A Cmt is a sequence of comment lines, one element per line,
// copied verbatim from the source. A blank line becomes an
// empty string. Pos points at the first line.
type Cmt struct {
	Text []string `json:"cmt"`
	Pos  Pos      `json:"pos"`
}

// An Item is a map key-value pair, a list element, or the
// root value. The key is always present for map items,
// but it is also be present in dashed lists that have an
// map line like `- key: "value"`.
//
// The comments sit in source order:
//
//   - PreCmt holds comments right before the item
//   - HeadCmt holds comments inside the item's block at the top
//   - InCmt holds inline comments following the item
//   - FootCmt holds comments inside the item's blcok at the bottom
//   - PostCmt holds comments right after the item
//
// For HeadCmt and FootCmt to be recorded as such, the comments
// must be spaced from block values by a blank line.
//
// Indent is the indent added for the content under this item.
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

// A List holds a list of items. Block defines if the list is
// inline or not, and Dashed defines if the Block form is
// expressed as a compact list of dashed items, or otherwise
// the fence form that allows block children. A list Item will
// have its key set for dashed key-value pairs (`- key: "value"`).
type List struct {
	Items  []*Item `json:"list"`
	Dashed bool    `json:"dashed,omitzero"`
	Block  bool    `json:"block,omitzero"`
	Pos    Pos     `json:"pos"`
}

func (*List) value() {}

// A Map holds an ordered sequence of key-value pairs.
// Block defines if the map is inline or multi-line.
type Map struct {
	Items []*Item `json:"map"`
	Block bool    `json:"block,omitzero"`
	Pos   Pos     `json:"pos"`
}

func (*Map) value() {}
