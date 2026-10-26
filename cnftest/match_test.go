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

package cnftest

import (
	"errors"
	"strings"
	"testing"
)

func TestMatchErrorExact(t *testing.T) {
	c := &Case{ToJSON: Directive{Kind: "error"}, Error: Data{Text: "line 1: bad thing"}}
	ok, err := MatchError(c, "", errors.New("line 1: bad thing"))
	if err != nil || !ok {
		t.Errorf("exact match: ok=%v err=%v", ok, err)
	}
	ok, err = MatchError(c, "", errors.New("line 2: bad thing"))
	if err != nil || ok {
		t.Errorf("exact mismatch: ok=%v err=%v", ok, err)
	}
}

func TestMatchErrorRegex(t *testing.T) {
	c := &Case{ToJSON: Directive{Kind: "error"}, Error: Data{Text: `^line 1: cannot (parse value|find key-value separator) .*$`, Regexp: true}}
	ok, err := MatchError(c, "", errors.New(`line 1: cannot parse value "a"`))
	if err != nil || !ok {
		t.Errorf("regex match: ok=%v err=%v", ok, err)
	}
	ok, err = MatchError(c, "", errors.New("line 1: cannot parse"))
	if err != nil || ok {
		t.Errorf("regex partial must not match (anchored): ok=%v err=%v", ok, err)
	}
}

func TestMatchErrorLineVar(t *testing.T) {
	block := "a:\n    b: 1\n      c: 2\n"
	c := &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "line ${a:l}: cannot mix indentation prefixes"},
		Vars:   map[string]Data{"a": {Text: `^( +)c:`, Regexp: true}},
	}
	// The c pair sits on line 3.
	ok, err := MatchError(c, block, errors.New("line 3: cannot mix indentation prefixes"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("line var: expected match")
	}
	ok, err = MatchError(c, block, errors.New("line 2: cannot mix indentation prefixes"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("line var: wrong line must not match")
	}
}

func TestMatchErrorColumnVar(t *testing.T) {
	block := "a: 1\nb: 2\n"
	c := &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "line ${a:l}, column ${a:c}: bad value"},
		Vars:   map[string]Data{"a": {Text: `^(b)`, Regexp: true}},
	}
	// b starts at line 2, column 1.
	ok, err := MatchError(c, block, errors.New("line 2, column 1: bad value"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("column var: expected match")
	}
}

func TestMatchErrorGroupPinsPosition(t *testing.T) {
	// The locator matches a prefix but the first group pins the
	// position inside the match.
	block := "key: value\n"
	c := &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "column ${a:c}"},
		Vars:   map[string]Data{"a": {Text: `key: (value)`, Regexp: true}},
	}
	// "value" starts at rune column 6.
	ok, err := MatchError(c, block, errors.New("column 6"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("group pin: expected column 6")
	}
}

func TestMatchErrorLCVar(t *testing.T) {
	// The lc reference joins the two numbers: the group's
	// line and column in one "L:C" spell.
	block := "first\nkey: value\n"
	c := &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "${a:lc}: bad value"},
		Vars:   map[string]Data{"a": {Text: `key: (value)`, Regexp: true}},
	}
	// "value" sits at line 2, rune column 6.
	ok, err := MatchError(c, block, errors.New("2:6: bad value"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("lc var: expected match")
	}
}

func TestMatchErrorMultilineVar(t *testing.T) {
	// The locator matches on any line, not only the first.
	block := "one\n\ntwo\n"
	c := &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "line ${a:l}"},
		Vars:   map[string]Data{"a": {Text: `(two)`, Regexp: true}},
	}
	ok, err := MatchError(c, block, errors.New("line 3"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("multiline var: expected line 3")
	}
}

func TestMatchErrorRuneColumns(t *testing.T) {
	// Columns count runes, not bytes: ✓ is three bytes but one
	// rune, so the column after it advances by one.
	block := "a: ✓\nb: x\n"
	c := &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "column ${a:c}"},
		Vars:   map[string]Data{"a": {Text: `(x)`, Regexp: true}},
	}
	ok, err := MatchError(c, block, errors.New("column 4"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("rune columns: expected column 4")
	}
}

func TestMatchErrorUndefinedVar(t *testing.T) {
	c := &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "line ${a:l}: bad"},
	}
	_, err := MatchError(c, "", errors.New("line 1: bad"))
	if err == nil {
		t.Error("expected error for a reference without a var")
	}
}

func TestMatchErrorVarNoMatch(t *testing.T) {
	c := &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "line ${a:l}: bad"},
		Vars:   map[string]Data{"a": {Text: `(missing)`, Regexp: true}},
	}
	_, err := MatchError(c, "a: 1\n", errors.New("line 1: bad"))
	if err == nil {
		t.Error("expected error for a locator that matches nothing")
	}
}

func TestMatchErrorHalfAnchoredLiteral(t *testing.T) {
	// Anchors carry no meaning anymore: a pattern that once read
	// as half-anchored is a literal now, and only that exact
	// message matches it.
	c := &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "^(unclosed$"},
	}
	ok, err := MatchError(c, "", errors.New("^(unclosed$"))
	if err != nil || !ok {
		t.Errorf("half-anchored literal: ok=%v err=%v", ok, err)
	}
	ok, err = MatchError(c, "", errors.New("unclosed"))
	if err != nil || ok {
		t.Errorf("half-anchored only exact: ok=%v err=%v", ok, err)
	}
}

func TestMatchErrorBadRegexp(t *testing.T) {
	c := &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "^(unclosed", Regexp: true},
	}
	_, err := MatchError(c, "", errors.New("anything"))
	if err == nil {
		t.Error("expected error for a bad error regexp")
	}
	c = &Case{
		ToJSON: Directive{Kind: "error"},
		Error:  Data{Text: "line ${a:l}"},
		Vars:   map[string]Data{"a": {Text: "(unclosed", Regexp: true}},
	}
	_, err = MatchError(c, "", errors.New("line 1"))
	if err == nil {
		t.Error("expected error for a bad locator regexp")
	}
}

func TestExpandVars(t *testing.T) {
	c := &Case{Vars: map[string]Data{"a": {Text: `(two)`, Regexp: true}}}
	source := "one\n\ntwo words\n"
	// The three forms: text, line, column.
	got, err := ExpandVars(c, source, "text ${a} at ${a:l}:${a:c}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "text two at 3:1" {
		t.Errorf("got %q", got)
	}
}

func TestExpandVarsGroupless(t *testing.T) {
	// A pattern without a group pins the whole match.
	c := &Case{Vars: map[string]Data{"a": {Text: `two`, Regexp: true}}}
	got, err := ExpandVars(c, "one two\n", "see ${a}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "see two" {
		t.Errorf("got %q", got)
	}
}

func TestExpandVarsMultipleUses(t *testing.T) {
	// One var referenced several times, in mixed forms.
	c := &Case{Vars: map[string]Data{"a": {Text: `^(b)`, Regexp: true}}}
	got, err := ExpandVars(c, "a: 1\nb: 2\n", "${a} on line ${a:l} column ${a:c}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "b on line 2 column 1" {
		t.Errorf("got %q", got)
	}
}

func TestExpandVarsNoVars(t *testing.T) {
	// A target without references passes through untouched.
	c := &Case{Vars: map[string]Data{"a": {Text: `(x)`, Regexp: true}}}
	got, err := ExpandVars(c, "x\n", "plain text")
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain text" {
		t.Errorf("got %q", got)
	}
}

func TestExpandVarsUndefinedReference(t *testing.T) {
	c := &Case{Vars: map[string]Data{"a": {Text: `(x)`, Regexp: true}}}
	_, err := ExpandVars(c, "x\n", "${a} and ${b}")
	if err == nil {
		t.Error("expected error for undefined ${b}")
	}
	// A ${ without a closing } is reported too.
	_, err = ExpandVars(c, "x\n", "${a} and ${b")
	if err == nil {
		t.Error("expected error for unterminated ${b")
	}
}

func TestExpandVarsLongerNameNotConfused(t *testing.T) {
	// ${ab} must not resolve as ${a} followed by a stray b.
	c := &Case{Vars: map[string]Data{"a": {Text: `(x)`, Regexp: true}, "ab": {Text: `(y)`, Regexp: true}}}
	got, err := ExpandVars(c, "y\n", "${ab}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "y" {
		t.Errorf("got %q", got)
	}
}

func TestExpandVarsLiteralMetacharacters(t *testing.T) {
	// A literal value holds its metacharacters inert: the pin
	// finds the text as written, not as a pattern. Under the
	// old all-regexp vars this value could not have been
	// spelled - the group would have been a syntax error, and
	// escaping it would have changed what the var says.
	c := &Case{Vars: map[string]Data{"a": {Text: "(x)"}}}
	got, err := ExpandVars(c, "a: (x)\n", "text ${a} at ${a:l}:${a:c}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "text (x) at 1:4" {
		t.Errorf("got %q", got)
	}
}

func TestExpandVarsLiteralWholeText(t *testing.T) {
	// The plain form of a literal takes the var's value
	// directly: the source is not searched, so a value that
	// never occurs in the source - a raw rune the source
	// spells as an escape - still substitutes.
	c := &Case{Vars: map[string]Data{"a": {Text: "✓"}}}
	got, err := ExpandVars(c, "a: \"\\u2713\"\n", "a: \"${a}\"")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a: \"✓\"" {
		t.Errorf("got %q", got)
	}
}

func TestUnquoteUnicodeEscape(t *testing.T) {
	// The header strings know the \u escape, four hex digits:
	// a case can spell a raw rune without the file holding it.
	s, err := unquote(`"\u2713"`)
	if err != nil {
		t.Fatal(err)
	}
	if s != "✓" {
		t.Errorf("got %q", s)
	}
	if _, err := unquote(`"\u271"`); err == nil {
		t.Error("expected error for \\u without four hex digits")
	}
	if _, err := unquote(`"\u27g3"`); err == nil {
		t.Error("expected error for \\u with a non-hex digit")
	}
}

func TestUnquoteUnicodeAstralEscape(t *testing.T) {
	// The uppercase \U takes eight hex digits, reaching the
	// astral planes the four-digit \u cannot.
	s, err := unquote(`"\U00010000"`)
	if err != nil {
		t.Fatal(err)
	}
	if s != "𐀀" {
		t.Errorf("got %q", s)
	}
	if _, err := unquote(`"\U00110000"`); err == nil {
		t.Error("expected error for \\U beyond U+10FFFF")
	}
	if _, err := unquote(`"\U0001000"`); err == nil {
		t.Error("expected error for \\U without eight hex digits")
	}
}

func TestExpandVarsUnknownForm(t *testing.T) {
	// A selector after the colon that is not one of the three
	// forms is a broken case: the reference is left in place
	// and the error names it.
	c := &Case{Vars: map[string]Data{"a": {Text: `(x)`, Regexp: true}}}
	_, err := ExpandVars(c, "x\n", "${a:rows}")
	if err == nil || !strings.Contains(err.Error(), "${a:rows}") {
		t.Errorf("expected error naming ${a:rows}, got %v", err)
	}
}
