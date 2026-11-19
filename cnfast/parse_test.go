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
	"reflect"
	"strings"
	"testing"
)

// The parse tests pin the byte-faithful contract at the single-
// document level: parsing one document produces an AST the render
// writes back exactly as it was read. The corpus cannot carry
// this check - the from-ast direction re-renders from the
// review form, which never records the string spellings - so
// the parse must be verified here, against the source itself.
//
// The fence continuations are the load-bearing half: a
// backslash at the end of a fence line swallows the newline,
// and the parse must record the exact line it read, so the
// render writes it back the same way. A parse that stores the
// decoded value as the recorded lines corrupts the pair: the
// render's stale check decodes Text, finds another value than
// Str, and refuses.

// TestParseRenderRoundTrip pins the byte-faithful contract for
// one document: parsing it, then rendering the result, must
// reproduce the source verbatim.
func TestParseRenderRoundTrip(t *testing.T) {
	var sources = []string{
		// a single quoted string
		"a: 'x'\n",

		// a single double-quoted string
		`a: "x"` + "\n",

		// an escaped string
		`a: "x\ny"` + "\n",

		// a block map
		"a:\n    b: 1\n",

		// a block list
		"- 1\n- 2\n",

		// a block fence with a mid-fence continuation: the
		// value keeps the newline of the first line
		"s:\n    \"\"\"\n    ab\\\n    cd\n    \"\"\"\n",

		// a block fence whose last line continues straight
		// into the closing delimiter: no trailing newline
		"s:\n    \"\"\"\n    one \\\n    two\\\n    \"\"\"\n",

		// a raw fence: no escapes, no continuations
		"s:\n    '''\n    ab\n    cd\n    '''\n",

		// a blank line deeper than the fence's prefix: the
		// excess whitespace is content
		"a:\n    \"\"\"\n    one\n     \n    \"\"\"\n",

		// an empty fence: no content lines at all
		"a:\n    \"\"\"\n    \"\"\"\n",

		// a compact fence: the interior records as one
		// element, its tail whatever the value says
		"a: \"\"\"it\"\"\"\n",
		"a: \"\"\"Multiple\\nlines.\"\"\"\n",
		"a: '''Single\\nline.'''\n",

		// an escaped string and a raw string: the quoted
		// families record too
		`a: "x\ny"` + "\n",
		"a: 'raw'\n",

		// comments after a nested list fence's close trail its
		// element, at the nested fence's indent
		"a:\n  ---\n    ---\n    0\n    ---\n    # comment\n  ---\n",
		"---\n  ---\n  0\n  ---\n  # c\n\n  # d\n---\n1\n---\n",

		// comments ahead of list fence elements of every kind stay
		// with the element they precede
		"---\n# c\n0\n---\n",
		"---\n# c\n\n0\n---\n# d\n1\n---\n",
		"---\n0\n---\n# d\na: 1\n---\n",
		"a:\n    ---\n    # c\n    '''\n    x\n    '''\n    ---\n# after\nb: 1\n",
	}
	for _, src := range sources {
		ast, err := Parse([]byte(src), nil)
		if err != nil {
			t.Errorf("parse %q: %s", src, err)
			continue
		}
		out, err := Render(ast, nil)
		if err != nil {
			t.Errorf("render %q: %s", src, err)
			continue
		}
		if string(out) != src {
			t.Errorf("round trip:\n got %q\nwant %q", out, src)
		}
	}
}

// TestNestedListFenceAfterBlank pins the nested list fence that
// opens after a blank line: the element records the fence's
// indent, so the render writes it nested and it reads back.
func TestNestedListFenceAfterBlank(t *testing.T) {
	src := "0:\n    ---\n\n        ---\n        0\n        ---\n    ---\n"
	ast, err := Parse([]byte(src), nil)
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	out, err := Render(ast, nil)
	if err != nil {
		t.Fatalf("render: %s", err)
	}
	back, err := Parse(out, nil)
	if err != nil {
		t.Fatalf("render %q does not parse back: %s", out, err)
	}
	outer := back.Value.(*Map).Items[0].Value.(*List)
	if len(outer.Items) != 1 || outer.Items[0].Indent != "    " {
		t.Fatalf("render %q lost the nested fence", out)
	}
}

// TestEmptyListFenceElementAfterBlank pins the error for an element
// holding only blank or comment lines: it names the delimiter that
// opened the element, as it does for an element holding nothing.
func TestEmptyListFenceElementAfterBlank(t *testing.T) {
	for _, src := range []string{
		"0:\n    ---\n    []\n    ---\n\n    ---\n",
		"0:\n    ---\n    []\n    ---\n    # c\n    ---\n",
	} {
		_, err := Parse([]byte(src), nil)
		if want := "4:5: cannot parse empty list fence element"; err == nil || err.Error() != want {
			t.Errorf("parse %q: err %v, want %q", src, err, want)
		}
	}
}

// TestNestedFenceIndentAfterBlank pins the shared indent of nested
// fences when one opens after a blank: the check reads the element's
// first structural line, not the blank ahead of it.
func TestNestedFenceIndentAfterBlank(t *testing.T) {
	src := "a:\n  ---\n    ---\n    1\n    ---\n  ---\n\n      ---\n      2\n      ---\n  ---\n"
	_, err := Parse([]byte(src), nil)
	if want := "8:7: cannot mix indents inside list fence"; err == nil || err.Error() != want {
		t.Errorf("parse %q: err %v, want %q", src, err, want)
	}
}

// TestParseRecords pins the recorded elements of the strings
// the round trip walks: the element grammar - every element
// closed by its own terminator or marker, the compact
// interior standing alone - is the contract the render and
// the wrap both build on.
func TestParseRecords(t *testing.T) {
	var cases = []struct {
		src  string
		str  string
		text []string
	}{{
		// a mid-fence continuation: the marker eats the
		// newline, the next element continues the value
		src:  "s:\n    \"\"\"\n    ab\\\n    cd\n    \"\"\"\n",
		str:  "abcd\n",
		text: []string{"ab\\", "cd\n"},
	}, {
		// a continuation into the close: the last element
		// carries the marker, no terminator
		src:  "s:\n    \"\"\"\n    one \\\n    two\\\n    \"\"\"\n",
		str:  "one two",
		text: []string{"one \\", "two\\"},
	}, {
		// a blank content line: its whitespace is the
		// element, the terminator its own
		src:  "a:\n    \"\"\"\n    one\n     \n    \"\"\"\n",
		str:  "one\n \n",
		text: []string{"one\n", " \n"},
	}, {
		// a compact fence: the interior is one element,
		// its tail whatever the value says
		src:  "a: \"\"\"Multiple\\nlines.\"\"\"\n",
		str:  "Multiple\nlines.",
		text: []string{`Multiple\nlines.`},
	}, {
		// a raw compact fence: the interior raw, one
		// element, no escapes
		src:  "a: '''Single\\nline.'''\n",
		str:  `Single\nline.`,
		text: []string{`Single\nline.`},
	}}
	for _, tc := range cases {
		ast, err := Parse([]byte(tc.src), nil)
		if err != nil {
			t.Errorf("parse %q: %s", tc.src, err)
			continue
		}
		s := ast.Value.(*Map).Items[0].Value.(*Str)
		if s.Data != tc.str {
			t.Errorf("%q: Str=%q want %q", tc.src, s.Data, tc.str)
		}
		if !reflect.DeepEqual(s.Text, tc.text) {
			t.Errorf("%q: Text=%q want %q", tc.src, s.Text, tc.text)
		}
		// The recorded elements decode to the very value:
		// Text is the spelling the render writes back, so
		// its decode must agree with what the parse stored
		// in Str.
		var b strings.Builder
		if err := s.decode(&b); err != nil {
			t.Errorf("%q: decode: %s", tc.src, err)
			continue
		}
		if b.String() != tc.str {
			t.Errorf("%q: decoded=%q want %q", tc.src, b.String(), tc.str)
		}
	}
}
