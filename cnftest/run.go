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
	"strings"
	"testing"
)

// A Skip is a harness's refusal to run a direction: the
// reason in its own words, since only it knows why it runs
// the directions it does. Convert returns it for a direction
// the harness does not run; the dispatcher skips that
// direction quietly, or reports the reason when the case has
// no other direction to run.
type Skip struct {
	Reason string
}

func (s Skip) Error() string { return s.Reason }

// A Harness is one test binary's registration against the
// corpus: the conversion it runs for each direction, and the
// assertion it runs for each comparison.
type Harness struct {

	// Convert runs one direction - "to-json", "from-json",
	// "to-ast", "from-ast" - reading the source text and
	// producing the target text, or failing with the error
	// the case's error directive advises. The reformat settings
	// are the case's reformat: block, for the harness to map
	// onto its own render options. A direction the harness
	// does not run returns a Skip.
	Convert func(direction, source string, reformat Reformat) (string, error)

	// Assert decides whether a direction's output matches
	// its expectation, both in the direction's target format:
	// "json", "cnf", or "ast". The kind is the directive's -
	// the comparison is per-harness, so the kind routing
	// lives here. A nil return means equal; a non-nil error
	// is the mismatch report, printed under the direction's
	// name.
	Assert func(format, kind, want, got string) error
}

// RunTests runs the corpus against one harness: every case of
// every topic, each through the directions it declares. The
// subtests are named topic/case, so a failure names its case;
// every diagnostic carries the same name too, so a line
// grepped out of the run still names its case. A case whose
// every direction the harness skips is skipped with the
// harness's reason.
func RunTests(t *testing.T, dir string, h Harness) {
	topics, err := ReadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range topics {
		for _, c := range topic.Cases {
			name := topic.Name + "/" + c.Name
			t.Run(name, func(t *testing.T) {
				runCaseOn(&tSpy{t: t}, name, c, h)
			})
		}
	}
}

// The directions a case may declare, in the order they run.
var directions = []string{"to-json", "from-json", "to-ast", "from-ast"}

// runCaseOn executes one case through the directions it
// declares, against any recorder: the real run records into a
// *testing.T, the dispatcher's own tests into a spy. Each
// declared direction converts its inputs and asserts the
// outputs; an error-kind directive expects the conversion to
// fail with the case's advised message. A direction the
// harness skips is quiet; a case whose every direction is
// skipped reports the skip's reason. Every diagnostic carries
// the case's name, so a line grepped out of the run still
// names its case.
func runCaseOn(r recorder, name string, c *Case, h Harness) {
	ran := false
	for _, dir := range directions {
		d := c.directive(dir)
		if d.Kind == "" {
			continue
		}
		if runDirectionOn(r, name, c, dir, d, h) {
			ran = true
		}
	}
	if !ran {
		r.Skipf("%s: skipping unsupported formats case: %s", name, c.formats())
	}
}

// runDirectionOn executes one direction of one case: the
// inputs through the harness's Convert, the outputs against
// the first expectation block through its Assert. Every
// diagnostic carries the case's name and the direction, so a
// line grepped out of the run still names its case. It
// reports whether the direction ran - false when the harness
// skipped it, true otherwise, errors included.
func runDirectionOn(r recorder, name string, c *Case, dir string, d Directive, h Harness) bool {
	if !knownKind(d.Kind) {
		r.Errorf("%s: %s: unknown directive kind %q", name, dir, d.Kind)
		return true
	}
	inputs, expectations, berr := c.blocks(dir, h.Convert)
	if berr != nil {
		if _, ok := berr.(Skip); ok {
			// The harness refuses every input of the
			// direction: a skip, not a broken case. The
			// reasons reach the runner's skip bookkeeping
			// through the all-skipped case in runCaseOn,
			// which reports the first refusal's reason.
			return false
		}
		r.Errorf("%s: %s: broken case: %s", name, dir, berr)
		return true
	}
	// The input blocks take the vars too: a block that serves
	// one direction as its expectation serves the reverse as
	// its input, and the two see the same references.
	for i := range inputs {
		x, xerr := ExpandVars(c, inputs[i], inputs[i])
		if xerr != nil {
			r.Errorf("%s: %s: %s block #%d: broken case: %s", name, dir, sourceName(dir), i+1, xerr)
			return true
		}
		inputs[i] = x
	}
	skip := Skip{}
	for _, in := range inputs {
		_, err := h.Convert(dir, in, c.Reformat)
		if s, ok := err.(Skip); ok {
			skip = s
			break
		}
	}
	if (Skip{}) != skip {
		return false
	}
	if d.IsError() {
		for i, in := range inputs {
			_, err := h.Convert(dir, in, c.Reformat)
			if err == nil {
				r.Errorf("%s: %s: %s block #%d: no error, want %q", name, dir, sourceName(dir), i+1, c.Error.Text)
				continue
			}
			ok, merr := MatchError(c, in, err)
			if merr != nil {
				r.Errorf("%s: %s: %s block #%d: broken case: %s", name, dir, sourceName(dir), i+1, merr)
				continue
			}
			if !ok {
				want, xerr := ExpandVars(c, in, c.Error.Text)
				if xerr != nil {
					r.Errorf("%s: %s: %s block #%d: broken case: %s", name, dir, sourceName(dir), i+1, xerr)
					continue
				}
				r.Errorf("%s: %s: %s block #%d: error:\n  got  %q\n  want %q", name, dir, sourceName(dir), i+1, err.Error(), want)
			}
		}
		return true
	}
	if len(expectations) == 0 {
		r.Errorf("%s: %s needs a %s block", name, dir, expectationName(dir))
		return true
	}
	for i, in := range inputs {
		want, xerr := ExpandVars(c, in, expectations[0])
		if xerr != nil {
			r.Errorf("%s: %s: %s block #%d: broken case: %s", name, dir, sourceName(dir), i+1, xerr)
			continue
		}
		got, err := h.Convert(dir, in, c.Reformat)
		if err != nil {
			r.Errorf("%s: %s: %s block #%d: %s", name, dir, sourceName(dir), i+1, err)
			continue
		}
		if aerr := h.Assert(formatName(dir), d.Kind, want, got); aerr != nil {
			r.Errorf("%s: %s: %s block #%d: %s", name, dir, sourceName(dir), i+1, aerr)
		}
	}
	return true
}

// directive returns the case's directive for one direction.
func (c *Case) directive(dir string) Directive {
	switch dir {
	case "to-json":
		return c.ToJSON
	case "from-json":
		return c.FromJSON
	case "to-ast":
		return c.ToAST
	case "from-ast":
		return c.FromAST
	}
	return Directive{}
}

// knownKind reports whether a directive's kind is one of the
// corpus's kinds. An unknown kind is a case authoring bug: the
// reader keeps it, the run reports it - a typo like
// "regexp-error" must not fall through to the text path and
// demand a block it would never compare.
func knownKind(kind string) bool {
	switch kind {
	case "advised-text", "exact-value", "advised-value", "error":
		return true
	}
	return false
}

// blocks returns the input and expectation blocks of one
// direction: the inputs are the source format's blocks, the
// expectations the target format's. A parse-ast case's ast
// blocks are written as cnf: the from-ast inputs convert
// through the harness's own to-ast - the same conversion the
// cnf blocks get - so the corpus can author them readably.
// An entry the converter skips is dropped - the refusal is
// that entry's alone - and a case whose every entry refuses
// skips its whole direction, returned as the last Skip.
func (c *Case) blocks(dir string, convert func(direction, source string, reformat Reformat) (string, error)) (inputs, expectations []string, err error) {
	switch dir {
	case "to-json":
		return c.CNF, c.JSON, nil
	case "from-json":
		return c.JSON, c.CNF, nil
	case "to-ast":
		return c.CNF, c.AST, nil
	case "from-ast":
		if !c.ParseAST {
			return c.AST, c.CNF, nil
		}
		ast := make([]string, 0, len(c.AST))
		var skip error
		for _, block := range c.AST {
			out, cerr := convert("to-ast", block, Reformat{})
			if s, ok := cerr.(Skip); ok {
				skip = s // remember the refusal in case it is the only one
				continue
			}
			if cerr != nil {
				return nil, nil, cerr
			}
			ast = append(ast, out)
		}
		if len(ast) == 0 && len(c.AST) > 0 {
			// Every entry refused: the direction has nothing
			// to run, and the runner reports the refusal.
			return nil, nil, skip
		}
		return ast, c.CNF, nil
	}
	return nil, nil, nil
}

// sourceName maps a direction to its source format: the
// format of the input blocks it converts.
func sourceName(dir string) string {
	switch dir {
	case "to-json", "to-ast":
		return "cnf"
	case "from-json":
		return "json"
	case "from-ast":
		return "ast"
	}
	return "source"
}

// formatName maps a direction to its target format: the
// format both of its output and of its expectation block.
func formatName(dir string) string {
	switch dir {
	case "to-json":
		return "json"
	case "from-json", "from-ast":
		return "cnf"
	case "to-ast":
		return "ast"
	}
	return "expectation"
}

// expectationName maps a direction to the block name its
// expectations live in.
func expectationName(dir string) string {
	switch dir {
	case "to-json":
		return "json"
	case "from-json", "from-ast":
		return "cnf"
	case "to-ast":
		return "ast"
	}
	return "expectation"
}

// formats returns the names of the formats the case's
// declared directions read, ", "-joined: the formats the
// case is about, named in a skipped case's report.
func (c *Case) formats() string {
	var names []string
	for _, dir := range directions {
		if c.directive(dir).Kind != "" {
			names = append(names, sourceName(dir))
		}
	}
	return strings.Join(names, ", ")
}

// The recorder interface the dispatcher's self-tests use:
// the real run records into a *testing.T through tSpy, the
// tests into a spy.
type recorder interface {
	Skipf(format string, args ...any)
	Errorf(format string, args ...any)
}

// tSpy adapts a *testing.T to the recorder interface.
type tSpy struct{ t *testing.T }

func (s *tSpy) Skipf(format string, args ...any)  { s.t.Skipf(format, args...) }
func (s *tSpy) Errorf(format string, args ...any) { s.t.Errorf(format, args...) }
