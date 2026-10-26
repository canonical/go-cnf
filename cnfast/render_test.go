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
	"strings"
	"testing"
)

// The render tests pin the Text contract's four rules through
// the writer: fresh encodes when Text is unset, the recorded
// spelling rendered verbatim when the decode matches, the
// recorded spelling rendered unverified when Str is empty,
// and the stale refusal when the two disagree. The fence
// layout writes recorded elements verbatim; the inline layout
// respells the verified content, escaping whatever the fresh
// spelling needs.

// TestSpell pins one spelling per family, and the escalations
// an ask that cannot hold the string takes: never an error,
// always a family that holds.
func TestSpell(t *testing.T) {
	for _, tc := range []struct {
		want string
		str  string
		q    QuoteKind
	}{
		// the asked family, held
		{"a-b_9", "a-b_9", Unquoted},
		{`'raw'`, "raw", SingleQuoted},
		{`"esc\"aped"`, `esc"aped`, DoubleQuoted},
		{"'''it's'''", "it's", SingleFenced},
		{`"""x\ny"""`, "x\ny", DoubleFenced},

		// auto: bare, else the cheapest legal family
		{"bare", "bare", Unquoted},
		{`'has "dq"'`, `has "dq"`, Unquoted},
		{`"has 'sq'"`, "has 'sq'", Unquoted},

		// escalation: the ask cannot hold the string
		{`"can't"`, "can't", SingleQuoted}, // ' cannot hold a quote
		{`"two\nlines"`, "two\nlines", SingleQuoted},
		{`"""a'''b"""`, "a'''b", SingleFenced},                 // ''' cannot hold '''
		{`"""Multi " and '"""`, `Multi " and '`, SingleFenced}, // nor a final quote

		// the compact escaped close: the final quote escapes
		{`"""it\""""`, `it"`, DoubleFenced},
	} {
		if got := Quote(tc.str, tc.q); got != tc.want {
			t.Errorf("Spell(%q, %d)=%q want %q", tc.str, tc.q, got, tc.want)
		}
	}
}

// TestWriteStrRules walks the four rules through the writer.
func TestWriteStrRules(t *testing.T) {
	// Rule 1: no Text, empty Str - the empty fence writes no
	// content lines.
	s := &Str{Data: "", Quote: DoubleFenced, Block: true}
	var b strings.Builder
	if err := writeStr(&b, s, "    "); err != nil {
		t.Fatalf("rule 1: %s", err)
	}
	if want := "    \"\"\"\n    \"\"\"\n"; b.String() != want {
		t.Errorf("rule 1:\n got %q\nwant %q", b.String(), want)
	}

	// Rule 2: no Text, a set Str - the content spells fresh.
	b.Reset()
	s = &Str{Data: "one\ntwo\n", Quote: DoubleFenced, Block: true}
	if err := writeStr(&b, s, "    "); err != nil {
		t.Fatalf("rule 2: %s", err)
	}
	if want := "    \"\"\"\n    one\n    two\n    \"\"\"\n"; b.String() != want {
		t.Errorf("rule 2:\n got %q\nwant %q", b.String(), want)
	}

	// Rule 3: a set Text, a matching Str - the recorded
	// elements render verbatim, escapes and tails as written.
	b.Reset()
	s = &Str{Data: "esc\naped\n", Quote: DoubleFenced, Block: true,
		Text: []string{`esc\naped` + "\n"}}
	if err := writeStr(&b, s, "    "); err != nil {
		t.Fatalf("rule 3: %s", err)
	}
	if want := "    \"\"\"\n    esc\\naped\n    \"\"\"\n"; b.String() != want {
		t.Errorf("rule 3:\n got %q\nwant %q", b.String(), want)
	}

	// Rule 3, stale: the decode disagrees with Str - the
	// render refuses.
	b.Reset()
	s = &Str{Data: "changed", Quote: DoubleFenced, Block: true, Text: []string{"one two\n"}}
	if err := writeStr(&b, s, "    "); err == nil {
		t.Error("rule 3 stale: no error")
	}

	// Rule 4: a set Text, an empty Str - the recorded spelling
	// renders unverified.
	b.Reset()
	s = &Str{Data: "", Quote: DoubleFenced, Block: true, Text: []string{"recorded only\n"}}
	if err := writeStr(&b, s, "    "); err != nil {
		t.Fatalf("rule 4: %s", err)
	}
	if want := "    \"\"\"\n    recorded only\n    \"\"\"\n"; b.String() != want {
		t.Errorf("rule 4:\n got %q\nwant %q", b.String(), want)
	}
}

// TestWriteStrInlineRespells pins the inline layout of a
// recorded multi-line string: the compact close escapes the
// content's newlines, so a fence folded inline still decodes
// to the same value.
func TestWriteStrInlineRespells(t *testing.T) {
	s := &Str{Data: "ab\ncd\n", Quote: SingleFenced,
		Text: []string{"ab\n", "cd\n"}}
	var b strings.Builder
	if err := writeStr(&b, s, ""); err != nil {
		t.Fatalf("%s", err)
	}
	// The raw compact family cannot hold a newline: escalation
	// to the escaped family.
	if want := `"""ab\ncd\n"""`; b.String() != want {
		t.Errorf("inline:\n got %q\nwant %q", b.String(), want)
	}
}

// TestWriteStrStaleInline pins the four rules' refusal: a
// Str changed without Set - the recorded spelling left
// standing against a moved-on value - renders as an error,
// the render refusing to guess which of the two is current.
func TestWriteStrInlineRecorded(t *testing.T) {
	s := &Str{Data: `it"`, Quote: DoubleFenced, Text: []string{`it\"`}}
	var b strings.Builder
	if err := writeStr(&b, s, ""); err != nil {
		t.Fatalf("%s", err)
	}
	if want := `"""it\""""`; b.String() != want {
		t.Errorf("inline recorded:\n got %q\nwant %q", b.String(), want)
	}
}

// TestRenderBareWordValue pins value-position auto: the
// Unquoted family in value position takes the value policy -
// double by default, single when the content holds a quote or
// a backslash it spells raw - and a single the content cannot
// hold escalates to double.
func TestRenderBareWordValue(t *testing.T) {
	s := &Str{Data: "has spaces", Quote: Unquoted}
	var b strings.Builder
	if err := writeStr(&b, s, ""); err != nil {
		t.Fatalf("%s", err)
	}
	if want := `"has spaces"`; b.String() != want {
		t.Errorf("bare value:\n got %q\nwant %q", b.String(), want)
	}

	s = &Str{Data: "can't", Quote: Unquoted}
	b.Reset()
	if err := writeStr(&b, s, ""); err != nil {
		t.Fatalf("%s", err)
	}
	if want := `"can't"`; b.String() != want {
		t.Errorf("bare value quote:\n got %q\nwant %q", b.String(), want)
	}

	s = &Str{Data: `has "dq" and \bs`, Quote: Unquoted}
	b.Reset()
	if err := writeStr(&b, s, ""); err != nil {
		t.Fatalf("%s", err)
	}
	if want := `'has "dq" and \bs'`; b.String() != want {
		t.Errorf("bare value single:\n got %q\nwant %q", b.String(), want)
	}
}

// TestWriteStrClosingLine pins the fresh block synthesis
// against the delimiter hazard and the shape rule: a display
// line equal to the escaped delimiter would close the fence
// early on re-parse, so the render escapes one quote in it -
// the line decodes back to the same value; a raw-family line
// equal to its delimiter escalates the whole render to the
// escaped family; and a compact record demanded as a block
// respells, the record having been made for the inline
// shape.
func TestWriteStrClosingLine(t *testing.T) {
	for _, tc := range []struct {
		str   string
		quote QuoteKind
		block bool
		text  []string
		want  string
	}{
		{
			// A line equal to the escaped delimiter: one
			// quote escapes, the fence stays open.
			str:   "before\n\"\"\"\nafter\n",
			quote: DoubleFenced,
			block: true,
			want:  "    \"\"\"\n    before\n    \\\"\"\"\n    after\n    \"\"\"\n",
		},
		{
			// A raw-family value with a delimiter-equal
			// line: the render escalates to the escaped
			// family, whose delimiter differs from the
			// line.
			str:   "one\n'''\ntwo\n",
			quote: SingleFenced,
			block: true,
			want:  "    \"\"\"\n    one\n    '''\n    two\n    \"\"\"\n",
		},
		{
			// A compact record (one element, no tail)
			// demanded as a block: the content respells, a
			// marker holding the value's missing final
			// newline away.
			str:   "it\"",
			quote: DoubleFenced,
			block: true,
			text:  []string{`it\"`},
			want:  "    \"\"\"\n    it\"\\\n    \"\"\"\n",
		},
	} {
		s := &Str{Data: tc.str, Quote: tc.quote, Block: tc.block, Text: tc.text}
		var b strings.Builder
		if err := writeStr(&b, s, "    "); err != nil {
			t.Errorf("%q: %s", tc.str, err)
			continue
		}
		if b.String() != tc.want {
			t.Errorf("%q:\n got %q\nwant %q", tc.str, b.String(), tc.want)
			continue
		}
		// The written block re-parses to the same value.
		doc := "a:\n" + b.String()
		ast, err := Parse([]byte(doc), nil)
		if err != nil {
			t.Errorf("%q: re-parse: %s", tc.str, err)
			continue
		}
		got := ast.(*Item).Value.(*Map).Items[0].Value.(*Str).Data
		if got != tc.str {
			t.Errorf("%q: re-parsed value %q, the value changed", tc.str, got)
		}
	}
}
