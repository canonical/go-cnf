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

package cnfjson_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/canonical/go-cnf/cnfjson"
	"github.com/canonical/go-cnf/cnftest"
)

// TestCases runs the corpus against cnfjson: to-json parses
// each cnf block and renders it as JSON. The other directions
// skip: the package renders, it does not parse JSON or write
// cnf.
func TestCases(t *testing.T) {
	cnftest.RunTests(t, "../spec/cases", cnftest.Harness{
		Convert: convert,
		Assert:  assert,
	})
}

// skip is cnfjson's refusal of one direction it does not run.
func skip(direction string) error {
	return cnftest.Skip{Reason: "unsupported direction: " + direction}
}

// convert runs one direction: to-json parses the cnf and
// renders the AST as JSON; from-json parses the JSON and
// renders the AST as cnf. The other directions skip.
func convert(direction, source string, reformat cnftest.Reformat) (string, error) {
	switch direction {
	case "to-json":
		out, err := cnfjson.ToJSON([]byte(source))
		if err != nil {
			return "", err
		}
		return string(out), nil
	case "from-json":
		out, err := cnfjson.FromJSON([]byte(source))
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
	return "", skip(direction)
}

// assert decides whether a direction's output matches its
// expectation, both in the direction's target format. The json
// compares after trimming the whitespace between tokens - the
// case files write their json blocks pretty-printed, cnfjson
// emits compact JSON, and the JSON grammar allows no newline
// inside a string value, so the trim touches nothing but the
// layout. A cnf exact-value kind compares on the value: both
// sides translate back to JSON, where the spelling choices the
// two from-json readings may make differently disappear.
func assert(format, kind, want, got string) error {
	if format == "json" {
		want, got = trimJSON(want), trimJSON(got)
	}
	if format == "cnf" && kind == "exact-value" {
		return sameValueJSON(want, got)
	}
	if strings.TrimSuffix(got, "\n") != strings.TrimSuffix(want, "\n") {
		return mismatch(want, got)
	}
	return nil
}

// sameValueJSON compares two cnf texts on their value: both
// translate to JSON, and the JSON texts compare after the
// usual trim. A side that does not re-translate is a broken
// output, not a mismatch.
func sameValueJSON(want, got string) error {
	gotJSON, err := cnfjson.ToJSON([]byte(got))
	if err != nil {
		return fmt.Errorf("cnf does not re-translate: %s", err)
	}
	wantJSON, err := cnfjson.ToJSON([]byte(want))
	if err != nil {
		return fmt.Errorf("cnf block does not translate: %s", err)
	}
	wantText, gotText := trimJSON(string(wantJSON)), trimJSON(string(gotJSON))
	if wantText != gotText {
		return mismatch(wantText, gotText)
	}
	return nil
}

// trimJSON trims leading and trailing whitespace from each line
// of a JSON text and joins them into a single compact string,
// allowing case files to use pretty-printed JSON while cnfjson
// emits compact JSON.
func trimJSON(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "")
}

// mismatch reports one failed comparison.
func mismatch(want, got string) error {
	return fmt.Errorf("mismatch:\nwant:\n%s\ngot:\n%s", want, got)
}
