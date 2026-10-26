package cnfast_test

import (
	"regexp"
	"testing"

	"github.com/canonical/now/internal/cnf/cnfast"
)

var loneCR = regexp.MustCompile("\\r($|[^\n])")

// FuzzParse pins the parse's contract under arbitrary input:
//
//   - Parse never panics, on any byte sequence.
//   - A parsed document renders: Render never fails on the
//     tree its own parse produced.
//   - The rendered document re-parses to the same value:
//     rendering is a fixed point for parsing.
//   - The re-parse renders identically: the rendered form is
//     stable, one render away at any depth.
//
// A parse error is a fine answer too - the corpus's error
// cases pin those - so the fuzz only reaches the invariants
// when the parse accepts.
func FuzzParse(f *testing.F) {
	f.Add("a: 1\n")
	f.Add("a:\n    b: [1, 2]\n")
	f.Add("---\n- one\n---\n")
	f.Add("a: '''fence\nlines.'''\n")
	f.Add("# comment\na: 1 # trailing\n")
	f.Add("[1, {a: b}, \"s\"]\n")
	f.Add("a.x.1: v\n")
	f.Add("")
	f.Add("\xef\xbb\xbfa: 1\n")

	f.Fuzz(func(t *testing.T, in string) {
		ast, err := cnfast.Parse([]byte(in), nil)
		if err != nil {
			return
		}
		out1, err := cnfast.Render(ast, nil)
		if err != nil {
			t.Fatalf("parse accepted %q but render fails: %s", in, err)
		}
		// A lone carriage return is content, so won't roundtrip.
		if loneCR.MatchString(in) {
			return
		}
		reparsed, err := cnfast.Parse(out1, nil)
		if !sameAST(ast, reparsed) {
			t.Fatalf("re-parse changed the value:\n src %q\ncnf %q", in, out1)
		}
		out2, err := cnfast.Render(reparsed, nil)
		if err != nil {
			t.Fatalf("re-parse does not render:\n src %q\ncnf %q\n err %s", in, out1, err)
		}
		if string(out1) != string(out2) {
			t.Fatalf("render is not stable:\n src %q\n one %q\n two %q", in, out1, out2)
		}
	})
}
