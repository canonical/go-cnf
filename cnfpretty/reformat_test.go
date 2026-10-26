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
	"reflect"
	"testing"

	"github.com/canonical/go-cnf/cnfast"
)

// The wrap tests pin the string wrapping directly, one wrap
// scenario at a time: the wrap is a re-spelling, the value
// never changing - the
// wrapped Text decoding back to the very Str it wrapped - the
// breaks spelled as continuations, the space before a soft
// break surviving in the representation, the hard break
// cutting mid-word, the last line's terminator escaped so the
// fence's decode reconstructs the value exactly.

// wrapCase is one wrap scenario: the value, the knobs, and
// the Text the wrap must produce.
type wrapCase struct {
	name string
	str  string
	soft int
	hard int
	text []string
}

var wrapCases = []wrapCase{{
	name: "soft alone never forces: no space past the floor, the line stands",
	str:  "one two three",
	soft: 10,
	text: []string{"one two three" + `\`},
}, {
	name: "soft alone breaks at the first space past the floor",
	str:  "one two three four",
	soft: 3,
	text: []string{"one " + `\`, "two " + `\`, "three " + `\`, "four" + `\`},
}, {
	name: "under the width stands",
	str:  "one two",
	soft: 10,
	text: nil,
}, {
	name: "hard alone breaks at the last space before the deadline",
	str:  "one two three",
	hard: 10,
	text: []string{"one two " + `\`, "three" + `\`},
}, {
	name: "hard alone cuts mid-word when no space before the deadline",
	str:  "onetwothree",
	hard: 7,
	text: []string{"onetwot" + `\`, "hree" + `\`},
}, {
	name: "the window: last space between soft and hard wins",
	str:  "one twothree four",
	soft: 8,
	hard: 11,
	text: []string{"one twothre" + `\`, "e four" + `\`},
}, {
	name: "the window with several spaces: the last one before hard wins",
	str:  "aaa bbb ccc ddd eee",
	soft: 4,
	hard: 15,
	text: []string{"aaa bbb ccc " + `\`, "ddd eee" + `\`},
}, {
	name: "the window skips early spaces: soft is the floor",
	str:  "aa bb ccccccccccccccc",
	soft: 12,
	hard: 20,
	text: []string{"aa bb cccccccccccccc" + `\`, "c" + `\`},
}, {
	name: "multi-line value wraps each line independently",
	str:  "one two three\nfour five",
	soft: 8,
	text: []string{"one two three\n", "four five" + `\`},
}, {
	name: "a literal backslash escapes in the record and decodes back",
	str:  `a\b one two three four`,
	soft: 5,
	text: []string{`a\\b one ` + `\`, `two three ` + `\`, "four" + `\`},
}, {
	name: "a quote stays raw inside the fence and the pieces still decode",
	str:  `it"s "quoted" one two`,
	soft: 5,
	text: []string{`it"s "quoted" ` + `\`, "one two" + `\`},
}, {
	name: "exactly at the width stands",
	str:  "one two three",
	soft: 13,
	text: nil,
}}

// TestWrapStr runs the wrap scenarios: each value wraps under
// its knobs, the produced Text matching the expectation, and
// - the load-bearing check - the Text decoding back to the
// very value, the wrap a pure re-spelling.
func TestWrapStr(t *testing.T) {
	for _, tc := range wrapCases {
		t.Run(tc.name, func(t *testing.T) {
			s := &cnfast.Str{Data: tc.str, Quote: cnfast.DoubleQuoted}
			p := Options{SoftWrapStringFromWidth: tc.soft, HardWrapStringFromWidth: tc.hard}
			wrapped := p.wrapStr(s)
			if tc.text == nil {
				if wrapped {
					t.Fatalf("wrapped a string within the widths: Text=%q", s.Text)
				}
				return
			}
			if !wrapped {
				t.Fatalf("did not wrap an over-width string")
			}
			if !reflect.DeepEqual(s.Text, tc.text) {
				t.Fatalf("Text=%q want %q", s.Text, tc.text)
			}
			// The value never changes: the Text decodes back
			// to the very Str the wrap wrapped.
			if err := s.Decode(); err != nil {
				t.Fatalf("decode error: %s", err)
			}
			if s.Data != tc.str {
				t.Fatalf("decoded=%q want the untouched value %q", s.Data, tc.str)
			}
			// The wrap flips the spelling: a fence holds the
			// lines, the escaped family carrying the
			// continuations.
			if !s.Block {
				t.Fatal("Block=false want the fence-block form")
			}
			if s.Quote != cnfast.DoubleFenced {
				t.Fatalf("Quote=%d want TripleFenced", s.Quote)
			}
		})
	}
}

// TestWrapRoundTrip renders a wrapped string and re-parses
// the output: the value survives the whole cycle - wrap,
// render, parse - byte-identical, the representation the only
// thing that moved.
func TestWrapRoundTrip(t *testing.T) {
	src := "a: \"one two three\"\n"
	ast, err := cnfast.Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	// The apply mutates the tree it is given, so the
	// round trip works on a clone.
	root := Clone(ast)
	opts := Options{SoftWrapStringFromWidth: 3, HardWrapStringFromWidth: 10}
	if err := Apply(root, &opts); err != nil {
		t.Fatal(err)
	}
	out, err := cnfast.Render(root, nil)
	if err != nil {
		t.Fatalf("render error: %s", err)
	}
	want := "a:\n    \"\"\"\n    one two \\\n    three\\\n    \"\"\"\n"
	if string(out) != want {
		t.Fatalf("rendered=%q want %q", out, want)
	}
	// The rendered cnf re-parses to the same value.
	reparsed, err := cnfast.Parse(out, nil)
	if err != nil {
		t.Fatalf("re-parse error: %s", err)
	}
	m := reparsed.Value.(*cnfast.Map)
	got := m.Items[0].Value.(*cnfast.Str).Data
	if got != "one two three" {
		t.Fatalf("re-parsed value=%q want %q", got, "one two three")
	}
}

// TestWrapStaleErrors pins the Text contract's refusal: a
// Str changed without Set - the recorded spelling left
// standing against a moved-on value - renders as an error,
// the render refusing to guess which of the two stands.
func TestWrapStaleErrors(t *testing.T) {
	s := &cnfast.Str{Data: "one two three", Quote: cnfast.DoubleQuoted}
	p := Options{SoftWrapStringFromWidth: 10}
	if !p.wrapStr(s) {
		t.Fatal("did not wrap")
	}
	// Someone changes the value without Set: the recorded
	// spelling is now stale.
	s.Data = "changed"
	_, err := cnfast.Render(&cnfast.Item{Value: &cnfast.Map{Items: []*cnfast.Item{{
		Key:   []*cnfast.Str{{Data: "a", Quote: cnfast.Unquoted}},
		Value: s,
	}}, Block: true}}, nil)
	if err == nil {
		t.Fatal("rendered a stale spelling without error")
	}
}

// TestSetResetsText pins the programmatic path: Set changes
// the value and resets the recorded spelling, the next render
// encoding the value fresh.
func TestSetResetsText(t *testing.T) {
	s := &cnfast.Str{Data: "one two three", Quote: cnfast.DoubleQuoted, Text: []string{"one two three"}}
	s.Set("changed")
	if s.Data != "changed" {
		t.Fatalf("Str=%q want %q", s.Data, "changed")
	}
	if s.Text != nil {
		t.Fatalf("Text=%q want nil", s.Text)
	}
}
