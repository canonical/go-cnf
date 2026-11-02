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
	"bytes"
	"encoding/json"
	"testing"

	"github.com/canonical/go-cnf/cnfembed"
	"github.com/canonical/go-cnf/cnfjson"
)

// FuzzToJSON pins the translation's contract under arbitrary
// input, differential against the reference implementation:
//
//   - ToJSON never panics, on any byte sequence.
//   - Whatever cnfjson answers - json or error, byte for
//     byte - cnfembed answers the same.
//   - Accepted json is well formed and holds one value:
//     a document, not a fragment or a sequence.
//
// A parse error is a fine answer too - the corpus's error
// cases pin those - so the well-formedness check only runs
// when both implementations accept the document.
func FuzzToJSON(f *testing.F) {
	f.Add("a: 1\n")
	f.Add("a:\n    b: [1, 2]\n")
	f.Add("---\n- one\n---\n")
	f.Add("a: '''fence\nlines.'''\n")
	f.Add("a: \"\"\"fence \\x41 \\n end\"\"\"\n")
	f.Add("b:\n\t'''\n\traw \t one\n\n\t\t\n\traw two\n\t'''\n")
	f.Add("a.\\u0078: 'single'\n")
	f.Add("# comment\na: 1 # trailing\n")
	f.Add("[1, {a: b}, \"s\"]\n")
	f.Add("a.x.1: v\n")
	f.Add("0x1f: 0b101 + junk\n")
	f.Add("\"quoted key\": \"quoted value\"\n")
	f.Add("a:\n  b:\n    c: -12.5e-3\n")
	f.Add("\"\n")
	f.Add("\xef\xbb\xbfa: 1\n")
	f.Add("a: \"\"\"one-line fence\"\"\"\n")
	f.Add("1e700")
	f.Add("")
	f.Add("\x00\x01\xff")

	f.Fuzz(func(t *testing.T, in string) {
		want, werr := cnfjson.ToJSON([]byte(in), nil)
		got, gerr := cnfembed.ToJSON([]byte(in), nil)

		if (werr == nil) != (gerr == nil) {
			t.Fatalf("error mismatch:\n src %q\ncnfjson=%v\ncnfembed=%v", in, werr, gerr)
		}
		if werr != nil {
			if werr.Error() != gerr.Error() {
				t.Fatalf("error text mismatch:\n src %q\ncnfjson=%q\ncnfembed=%q", in, werr, gerr)
			}
			return
		}
		if string(want) != string(got) {
			t.Fatalf("json mismatch:\n src %q\nwant %s\n got %s", in, want, got)
		}

		// Numbers stay as text - UseNumber - so a legal but
		// float-overflowing number like 1e700 is still well
		// formed json, spelled verbatim.
		dec := json.NewDecoder(bytes.NewReader(got))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("accepted document does not produce well-formed json:\n src %q\n json %s\n err %v", in, got, err)
		}
	})
}
