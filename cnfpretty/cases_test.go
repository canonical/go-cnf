package cnfpretty_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/canonical/now/internal/cnf/cnfast"
	"github.com/canonical/now/internal/cnf/cnfpretty"
	"github.com/canonical/now/internal/cnf/cnftest"
)

// TestCases runs the corpus against cnfpretty, the reformat:
// to-ast parses each cnf block into the AST and marshals it in
// the review form; from-ast decodes the first ast block,
// applies the case's reformat settings, and renders it back
// as cnf. The value directions do not run: the reformat
// reads syntax, not values.
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
// AST and renders it back as cnf, under the case's reformat
// settings. The value directions skip.
func convert(direction, source string, reformat cnftest.Reformat) (string, error) {
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
		out, err := renderPretty(ast, reformat)
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
	return "", skip(direction)
}

// renderPretty renders one AST under the case's reformat
// settings: the corpus's vocabulary mapped onto cnfpretty's
// Options, the tree cloned first - the reformat mutates what
// it is given - then applied, then rendered byte-faithfully.
func renderPretty(ast any, reformat cnftest.Reformat) ([]byte, error) {
	opts := cnfpretty.Options{
		UnfoldListFromItems:     reformat.UnfoldListFromItems,
		UnfoldMapFromItems:      reformat.UnfoldMapFromItems,
		FoldListUptoItems:       reformat.FoldListUptoItems,
		FoldMapUptoItems:        reformat.FoldMapUptoItems,
		FoldPathUptoDepth:       reformat.FoldPathUptoDepth,
		FoldPathFromDepth:       reformat.FoldPathFromDepth,
		UnfoldPathFromDepth:     reformat.UnfoldPathFromDepth,
		UnfoldPathUptoDepth:     reformat.UnfoldPathUptoDepth,
		SoftWrapStringFromWidth: reformat.SoftWrapStringFromWidth,
		HardWrapStringFromWidth: reformat.HardWrapStringFromWidth,
		Reindent:                reformat.Reindent,
		IgnoreComments:          reformat.IgnoreComments,
	}
	root := ast.(*cnfast.Item)
	root = cnfpretty.Clone(root)
	if err := cnfpretty.Apply(root, &opts); err != nil {
		return nil, err
	}
	return cnfast.Render(root, nil)
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
