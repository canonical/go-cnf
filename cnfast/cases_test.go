package cnfast_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/canonical/now/internal/cnf/cnfast"
	"github.com/canonical/now/internal/cnf/cnftest"
)

// TestCases runs the corpus against cnfast, the AST parser:
// to-ast parses each cnf block into the AST and marshals it in
// the review form; from-ast decodes the first ast block and
// renders it back as cnf. The value directions do not run:
// cnfast reads syntax, not values.
func TestCases(t *testing.T) {
	cnftest.RunTests(t, "../spec/cases", cnftest.Harness{
		Convert: convert,
		Assert:  assert,
	})
}

// skip is cnfast's refusal of one direction it does not run:
// the value directions, since it reads syntax, not values.
func skip(direction string) error {
	return cnftest.Skip{Reason: fmt.Sprintf("unsupported direction: %q", direction)}
}

// convert runs one direction: to-ast parses the cnf and
// marshals the AST in the review form; from-ast decodes the
// AST and renders it back as cnf, byte-faithfully - the
// reformat cases belong to cnfpretty, skipped here. The value
// directions skip.
func convert(direction, source string, reformat cnftest.Reformat) (string, error) {
	if !reformat.IsZero() {
		return "", skip("reformat")
	}
	switch direction {
	case "to-ast":
		ast, err := cnfast.Parse([]byte(source), nil)
		if err != nil {
			return "", err
		}
		out, err := cnfast.MarshalJSON(ast)
		if err != nil {
			return "", err
		}
		return string(out), nil
	case "from-ast":
		ast, err := cnfast.UnmarshalJSON([]byte(source))
		if err != nil {
			return "", err
		}
		out, err := cnfast.Render(ast, nil)
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
	return "", skip(direction)
}

// assert decides whether a direction's output matches its
// expectation, both in the direction's target format. The ast
// format compares verbatim: the review form is the value, so
// the marshaled text must agree exactly. The cnf format
// routes on the kind: the text kinds compare verbatim; the
// value kind re-parses both sides and matches the ASTs, since
// the rendered spelling may normalize layout the AST does not
// record.
func assert(format, kind, want, got string) error {
	switch format {
	case "ast":
		if strings.TrimSuffix(got, "\n") != strings.TrimSuffix(want, "\n") {
			return mismatch(want, got)
		}
		return nil
	case "cnf":
		switch kind {
		case "advised-value":
			gotAST, err := cnfast.Parse([]byte(got), nil)
			if err != nil {
				return fmt.Errorf("rendered cnf does not re-parse: %s", err)
			}
			wantAST, err := cnfast.Parse([]byte(want), nil)
			if err != nil {
				return fmt.Errorf("cnf block does not parse: %s", err)
			}
			if !sameAST(gotAST, wantAST) {
				gotJSON, _ := cnfast.MarshalJSON(gotAST)
				wantJSON, _ := cnfast.MarshalJSON(wantAST)
				return mismatch(string(wantJSON), string(gotJSON))
			}
			return nil
		default:
			if strings.TrimSuffix(got, "\n") != strings.TrimSuffix(want, "\n") {
				return mismatch(want, got)
			}
			return nil
		}
	}
	return fmt.Errorf("cannot assert format %q", format)
}

// mismatch reports two texts that should have agreed, each
// on its own lines with a | marking each line's start - the
// same convention the dispatcher's self-tests use - so a
// failure reads top-to-bottom and the indents stay honest.
func mismatch(want, got string) error {
	return fmt.Errorf("mismatch:\n  got:\n%s\n  want:\n%s", indent(got), indent(want))
}

// indent prefixes every line of s with a | after four spaces,
// marking where the line begins - meaningful here, since cnf
// content is indent dependent.
func indent(s string) string {
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = "    |" + ln
	}
	return strings.Join(lines, "\n")
}

// sameAST reports whether two ASTs hold the same value: the
// same structure and the same string, number, and scalar
// texts. Positions, indents, quote styles, and comment runs
// are spellings of the syntax, not the value, so they are
// ignored - the advised-value comparison is about what the
// AST decodes to, not how it was written.
func sameAST(a, b any) bool {
	switch av := a.(type) {
	case *cnfast.Item:
		// The root item: keyless, so only its value compares.
		bv, ok := b.(*cnfast.Item)
		return ok && sameAST(av.Value, bv.Value)
	case *cnfast.Map:
		bv, ok := b.(*cnfast.Map)
		if !ok || len(av.Items) != len(bv.Items) {
			return false
		}
		for i := range av.Items {
			ap, bp := av.Items[i], bv.Items[i]
			if len(ap.Key) != len(bp.Key) {
				return false
			}
			for j := range ap.Key {
				if ap.Key[j].Data != bp.Key[j].Data {
					return false
				}
			}
			if !sameAST(ap.Value, bp.Value) {
				return false
			}
		}
		return true
	case *cnfast.List:
		bv, ok := b.(*cnfast.List)
		if !ok || len(av.Items) != len(bv.Items) {
			return false
		}
		for i := range av.Items {
			if len(av.Items[i].Key) != len(bv.Items[i].Key) {
				return false
			}
			for j := range av.Items[i].Key {
				if av.Items[i].Key[j].Data != bv.Items[i].Key[j].Data {
					return false
				}
			}
			if !sameAST(av.Items[i].Value, bv.Items[i].Value) {
				return false
			}
		}
		return true
	case *cnfast.Str:
		bv, ok := b.(*cnfast.Str)
		return ok && av.Data == bv.Data
	case *cnfast.Num:
		bv, ok := b.(*cnfast.Num)
		return ok && av.Text == bv.Text
	case *cnfast.Scalar:
		bv, ok := b.(*cnfast.Scalar)
		return ok && av.Text == bv.Text
	}
	return false
}
