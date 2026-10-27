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

package cnfjson

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/canonical/go-cnf/cnfast"
)

// defaultIndent is the indent prefix one nesting level deep -
// the indent the corpus's from-json expectations spell.
const defaultIndent = "    "

// A jval is one JSON value kept as a neutral tree: the walk
// reads it, the builder shapes it into AST nodes. The numbers
// keep their original text so their forms survive.
type jval struct {
	kind jkind
	str  string   // jstring, decoded
	num  string   // jnumber, verbatim
	list []jval   // jlist
	keys []string // jmap, in document order
	vals map[string]jval
}

type jkind int

const (
	jnull jkind = iota
	jbool
	jnumber
	jstring
	jlist
	jmap
)

// Options for parsing and rendering.None exist yet,
// here for future compatibility.
type Options struct{}

// ParseJSON translates a json value to a cnf AST hierarchy.
func ParseJSON(data []byte, opts *Options) (*cnfast.Item, error) {
	root, err := walk(data)
	if err != nil {
		return nil, err
	}
	return &cnfast.Item{Value: shape(root, false)}, nil
}

// FromJSON translates json text to cnf text.
func FromJSON(data []byte, opts *Options) ([]byte, error) {
	root, err := ParseJSON(data, opts)
	if err != nil {
		return nil, err
	}
	return cnfast.Render(root, nil)
}

// walk reads one JSON document into the neutral tree, with a
// token-level decoder so duplicate keys are seen and rejected
// rather than silently resolved by last-wins.
func walk(data []byte) (jval, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	val, err := walkValue(dec)
	if err != nil {
		return jval{}, err
	}
	if _, err := dec.Token(); err == nil {
		return jval{}, fmt.Errorf("cannot parse trailing JSON values")
	}
	return val, nil
}

func walkValue(dec *json.Decoder) (jval, error) {
	tok, err := dec.Token()
	if err != nil {
		return jval{}, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return walkMap(dec)
		case '[':
			return walkList(dec)
		}
		return jval{}, fmt.Errorf("cannot parse unexpected delimiter %q", t)
	case json.Number:
		return jval{kind: jnumber, num: foldExponent(t.String())}, nil
	case string:
		return jval{kind: jstring, str: t}, nil
	case bool:
		return jval{kind: jbool, str: boolText(t)}, nil
	case nil:
		return jval{kind: jnull}, nil
	}
	return jval{}, fmt.Errorf("cannot parse unexpected token %T", tok)
}

func walkMap(dec *json.Decoder) (jval, error) {
	m := jval{kind: jmap, vals: map[string]jval{}}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return jval{}, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return jval{}, fmt.Errorf("cannot parse object key %T", keyTok)
		}
		if _, exists := m.vals[key]; exists {
			return jval{}, fmt.Errorf("cannot redefine key %q", key)
		}
		val, err := walkValue(dec)
		if err != nil {
			return jval{}, err
		}
		m.keys = append(m.keys, key)
		m.vals[key] = val
	}
	if _, err := dec.Token(); err != nil { // consume '}'
		return jval{}, err
	}
	return m, nil
}

func walkList(dec *json.Decoder) (jval, error) {
	l := jval{kind: jlist}
	for dec.More() {
		val, err := walkValue(dec)
		if err != nil {
			return jval{}, err
		}
		l.list = append(l.list, val)
	}
	if _, err := dec.Token(); err != nil { // consume ']'
		return jval{}, err
	}
	return l, nil
}

// boolText spells one boolean the way a cnf scalar holds it.
func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// shape builds one value node. The inline flag carries the
// context: an array holds only inline values, so everything
// under one inlines.
func shape(v jval, inline bool) cnfast.Value {
	switch v.kind {
	case jnull:
		return &cnfast.Scalar{Text: "null"}
	case jbool:
		return &cnfast.Scalar{Text: v.str}
	case jnumber:
		return &cnfast.Num{Text: v.num}
	case jstring:
		// The bare ask: the render's value-position auto settles
		// the family for a one-line value, and escalates a
		// multi-line one through its own ladder.
		return &cnfast.Str{Data: v.str, Quote: cnfast.Unquoted}
	case jlist:
		elems := make([]*cnfast.Item, len(v.list))
		for i, e := range v.list {
			elems[i] = &cnfast.Item{Value: shape(e, true)}
		}
		return &cnfast.List{Items: elems}
	case jmap:
		// The len guard is grammar, not formatting: a block map
		// with no pairs has no cnf spelling - the block branch
		// writes its pairs and nothing else. An empty object
		// stays inline, spelling {}.
		return &cnfast.Map{Items: shapePairs(v, inline), Block: !inline && len(v.keys) > 0}
	}
	return nil
}

// shapePairs builds the pair items of one map: one per pair,
// the key a single segment. A pair holding a block value
// records one indent step on its item - the layout this
// writer always uses - so the render places every level.
func shapePairs(v jval, inline bool) []*cnfast.Item {
	pairs := make([]*cnfast.Item, 0, len(v.keys))
	for _, k := range v.keys {
		val := shape(v.vals[k], inline)
		pair := &cnfast.Item{
			// The bare ask: the render's key-position ladder
			// spells a bare word bare, anything else by content
			// - the same escalation the values take.
			Key:   []*cnfast.Str{{Data: k, Quote: cnfast.Unquoted}},
			Value: val,
		}
		if isBlock(val) && !inline {
			pair.Indent = defaultIndent
		}
		pairs = append(pairs, pair)
	}
	return pairs
}

// isBlock reports whether a value's node carries one of the
// block forms: a block map, or a fence-block string.
func isBlock(v cnfast.Value) bool {
	switch n := v.(type) {
	case *cnfast.Map:
		return n.Block
	case *cnfast.Str:
		return n.Block
	}
	return false
}

// foldExponent lowercases the exponent letter of a JSON
// number's text: the cnf grammar reads e alone, and an E would
// spell a value the cnf parser rejects on re-read.
func foldExponent(num string) string {
	e := strings.IndexByte(num, 'E')
	if e < 0 {
		return num
	}
	return num[:e] + "e" + num[e+1:]
}
