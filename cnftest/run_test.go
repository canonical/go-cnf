package cnftest

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// A HarnessTest is one dispatcher behavior: a simulated case
// and the diagnostics the dispatcher owes for it, all in one
// place.
type HarnessTest struct {
	Name    string   // the subtest name
	Summary string   // what the row pins, one terse line
	Case    *Case    // the simulated corpus case
	Skips   []string // the expected skip messages
	Errors  []string // the expected error reports
}

// The fake harness the dispatcher tests use: Convert echoes
// its source, or fails with a message naming it, or skips a
// direction; Assert compares verbatim, reporting the texts
// on mismatch. The rows pin the dispatcher's plumbing, not
// any real format's conversion or equality.
var fake = Harness{
	Convert: func(dir, src string, reformat Reformat) (string, error) {
		switch dir {
		case "to-json", "from-json":
			return "", Skip{Reason: "value directions: not a value harness"}
		}
		return src, nil
	},
	Assert: func(format, kind, want, got string) error {
		if strings.TrimSuffix(got, "\n") != strings.TrimSuffix(want, "\n") {
			return fmt.Errorf("mismatch:\n  got  %q\n  want %q", got, want)
		}
		return nil
	},
}

// fakeFail builds a harness whose Convert fails for the given
// directions, with a message quoting its source.
func fakeFail(dirs ...string) Harness {
	h := fake
	h.Convert = func(dir, src string, reformat Reformat) (string, error) {
		for _, d := range dirs {
			if dir == d {
				return "", fmt.Errorf("boom: %s", src)
			}
		}
		switch dir {
		case "to-json", "from-json":
			return "", Skip{Reason: "value directions: not a value harness"}
		}
		return src, nil
	}
	return h
}

// HarnessTests pins the dispatcher's behaviors, one row each:
// the skips, the passes, and the reported mismatches - so a
// broken dispatcher fails here instead of silently passing
// real cases. The multi-line expectations carry a | marker:
// each line's content starts after it, so the literal indents
// with the code while the expected text keeps its exact
// spaces.
var HarnessTests = []HarnessTest{{
	Name:    "skip",
	Summary: "no ast directions: the case skips with the reason",
	Case:    &Case{},
	Skips:   []string{"case: skipping unsupported formats case: "},
}, {
	Name:    "to-ast",
	Summary: "the converted output matches the ast block: silence",
	Case: &Case{
		ToAST: Directive{Kind: "advised-text"},
		CNF:   []string{"a: 1\n"},
		AST:   []string{"a: 1\n"},
	},
}, {
	Name:    "to-ast-mismatch",
	Summary: "the conversion disagrees with the ast block: the diff reported",
	Case: &Case{
		ToAST: Directive{Kind: "advised-text"},
		CNF:   []string{"a: 1\n"},
		AST:   []string{"b: 2\n"},
	},
	Errors: []string{`
		|case: to-ast: cnf block #1: mismatch:
		|  got  "a: 1\n"
		|  want "b: 2\n"
	`},
}, {
	Name:    "to-ast-error",
	Summary: "the conversion fails with the advised message: silence",
	Case: &Case{
		ToAST: Directive{Kind: "error"},
		Error: Data{Text: "boom: a\n"},
		CNF:   []string{"a\n"},
	},
}, {
	Name:    "to-ast-no-error",
	Summary: "an error case whose input converts fine: the missing error reported",
	Case: &Case{
		ToAST: Directive{Kind: "error"},
		Error: Data{Text: "boom: a: 1\n"},
		CNF:   []string{"a: 1\n"},
	},
	Errors: []string{`
		|case: to-ast: cnf block #1: no error, want "boom: a: 1\n"
	`},
}, {
	Name:    "to-ast-no-block",
	Summary: "a to-ast without an ast block: the missing block reported",
	Case: &Case{
		ToAST: Directive{Kind: "advised-text"},
		CNF:   []string{"a: 1\n"},
	},
	Errors: []string{"case: to-ast needs a ast block"},
}, {
	Name:    "from-ast",
	Summary: "the converted output matches the cnf block: silence",
	Case: &Case{
		FromAST: Directive{Kind: "advised-text"},
		CNF:     []string{"a: 1\n"},
		AST:     []string{"a: 1\n"},
	},
}, {
	Name:    "from-ast-mismatch",
	Summary: "the conversion disagrees with the cnf block: the diff reported",
	Case: &Case{
		FromAST: Directive{Kind: "advised-text"},
		CNF:     []string{"a: 2\n"},
		AST:     []string{"a: 1\n"},
	},
	Errors: []string{`
		|case: from-ast: ast block #1: mismatch:
		|  got  "a: 1\n"
		|  want "a: 2\n"
	`},
}, {
	Name:    "from-ast-value",
	Summary: "the asserted values agree: silence",
	Case: &Case{
		FromAST: Directive{Kind: "advised-value"},
		CNF:     []string{"a: 1\n"},
		AST:     []string{"a: 1\n"},
	},
}, {
	Name:    "from-ast-value-mismatch",
	Summary: "the asserted values disagree: the diff reported",
	Case: &Case{
		FromAST: Directive{Kind: "advised-value"},
		CNF:     []string{"2\n"},
		AST:     []string{"1\n"},
	},
	Errors: []string{`
		|case: from-ast: ast block #1: mismatch:
		|  got  "1\n"
		|  want "2\n"
	`},
}, {
	Name:    "from-ast-error",
	Summary: "the conversion fails with the advised message: silence",
	Case: &Case{
		FromAST: Directive{Kind: "error"},
		Error:   Data{Text: "boom: /astConflict"},
		AST:     []string{"/astConflict"},
	},
}, {
	Name:    "from-ast-no-error",
	Summary: "an error case whose ast converts fine: the missing error reported",
	Case: &Case{
		FromAST: Directive{Kind: "error"},
		Error:   Data{Text: "boom: /astSimple"},
		AST:     []string{"/astSimple"},
	},
	Errors: []string{`
		|case: from-ast: ast block #1: no error, want "boom: /astSimple"
	`},
}, {
	Name:    "from-ast-no-block",
	Summary: "a from-ast without a cnf block: the missing block reported",
	Case: &Case{
		FromAST: Directive{Kind: "advised-text"},
		AST:     []string{"a: 1\n"},
	},
	Errors: []string{"case: from-ast needs a cnf block"},
}, {
	Name:    "to-json-skipped",
	Summary: "a value-only case: the case skips with the reason",
	Case: &Case{
		ToJSON: Directive{Kind: "advised-text"},
		CNF:    []string{"a: 1\n"},
		JSON:   []string{"{\"a\":1}"},
	},
	Skips: []string{"case: skipping unsupported formats case: cnf"},
}, {
	Name:    "mixed-runs",
	Summary: "a case with one run and one skipped direction: only the run reports",
	Case: &Case{
		ToJSON: Directive{Kind: "advised-text"},
		CNF:    []string{"a: 1\n"},
		JSON:   []string{"{\"a\":1}"},
		ToAST:  Directive{Kind: "advised-text"},
		AST:    []string{"a: 1\n"},
	},
}}

// TestHarness runs the simulated cases through the dispatcher
// and asserts its diagnostics, so a broken dispatcher fails
// here instead of silently passing real cases.
func TestHarness(t *testing.T) {
	for _, ht := range HarnessTests {
		t.Run(ht.Name, func(t *testing.T) {
			h := fake
			switch ht.Name {
			case "to-ast-error":
				h = fakeFail("to-ast")
			case "to-ast-no-error":
				// The row's premise: the conversion succeeds
				// where an error is advised.
			case "from-ast-error":
				h = fakeFail("from-ast")
			case "from-ast-no-error":
				// Same premise as to-ast-no-error.
			}
			s := &spy{}
			runCaseOn(s, "case", ht.Case, h)
			wantSkips := ht.Skips
			wantErrors := trimAll(ht.Errors)
			if !reflect.DeepEqual(s.skips, wantSkips) {
				t.Errorf("%s: skips:\n  got  %q\n  want %q", ht.Summary, s.skips, wantSkips)
			}
			if !reflect.DeepEqual(s.errors, wantErrors) {
				t.Errorf("%s: errors:\n  got  %q\n  want %q", ht.Summary, s.errors, wantErrors)
			}
		})
	}
}

// trimMarkers reads a marker-indented expectation: each line's
// content starts after it, so the literal indents with the
// code while the expected text keeps its exact spaces. The
// first and last lines - the literal's own quotes - carry no
// marker and are dropped. A line without a marker is an
// authoring bug and panics loudly.
func trimMarkers(s string) []string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines[1 : len(lines)-1] {
		i := strings.Index(ln, "|")
		if i < 0 {
			panic("expectation line without | marker: " + ln)
		}
		out = append(out, ln[i+1:])
	}
	return out
}

// trimAll strips the markers from every expectation. An empty
// list stays nil: the spy records nothing as nil, and
// DeepEqual distinguishes nil from the empty slice.
func trimAll(expectations []string) []string {
	if len(expectations) == 0 {
		return nil
	}
	out := make([]string, len(expectations))
	for i, e := range expectations {
		lines := strings.Split(e, "\n")
		if len(lines) < 2 || !strings.Contains(lines[1], "|") {
			out[i] = e // a plain one-line expectation, no markers
			continue
		}
		var b strings.Builder
		for j, ln := range trimMarkers(e) {
			if j > 0 {
				b.WriteString("\n")
			}
			b.WriteString(ln)
		}
		out[i] = b.String()
	}
	return out
}

// A spy is a recorder that remembers everything, for the
// dispatcher's tests: the skips and the errors, verbatim.
type spy struct {
	skips  []string
	errors []string
}

func (s *spy) Skipf(format string, args ...any) {
	s.skips = append(s.skips, fmt.Sprintf(format, args...))
}

func (s *spy) Errorf(format string, args ...any) {
	s.errors = append(s.errors, fmt.Sprintf(format, args...))
}
