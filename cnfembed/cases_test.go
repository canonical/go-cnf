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

package cnfembed_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/canonical/go-cnf/cnfembed"
	"github.com/canonical/go-cnf/cnftest"
)

// TestCases runs the corpus against cnfembed: to-json parses
// each cnf block and renders it as JSON. The other directions
// skip: the package renders, it does not parse JSON or write
// cnf.
func TestCases(t *testing.T) {
	cnftest.RunTests(t, "../spec/cases", cnftest.Harness{
		Convert: convert,
		Assert:  assert,
	})
}

// convert runs one direction: to-json parses the cnf and
// renders it as JSON. The other directions skip.
func convert(direction, source string, reformat cnftest.Reformat) (string, error) {
	if direction != "to-json" {
		return "", cnftest.Skip{Reason: "unsupported direction: " + direction}
	}
	out, err := cnfembed.ToJSON([]byte(source), nil)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// assert decides whether a direction's output matches its
// expectation, both in the direction's target format. The json
// compares after trimming the whitespace between tokens - the
// case files write their json blocks pretty-printed, cnfembed
// emits compact JSON, and the JSON grammar allows no newline
// inside a string value, so the trim touches nothing but the
// layout.
func assert(format, kind, want, got string) error {
	if format == "json" {
		want, got = trimJSON(want), trimJSON(got)
	}
	if strings.TrimSuffix(got, "\n") != strings.TrimSuffix(want, "\n") {
		return mismatch(want, got)
	}
	return nil
}

// trimJSON trims leading and trailing whitespace from each line
// of a JSON text and joins them into a single compact string,
// allowing case files to use pretty-printed JSON while cnfembed
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
