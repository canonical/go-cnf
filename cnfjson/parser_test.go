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
	"strings"
	"testing"

	"github.com/canonical/go-cnf/cnfast"
	"github.com/canonical/go-cnf/cnfjson"
)

// TestParseJSONRoundTrip pins the parse into the AST against
// the render back to JSON: a value that parses, renders as
// cnf, re-parses, and renders as JSON again must come back to
// the same JSON text - the whole cnf detour invisible. The
// table covers each JSON kind at the root, the container
// nestings, the empty forms, and the number forms kept
// verbatim through the trip.
func TestParseJSONRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		out  string // want, after trimJSON; "" means equal to in
	}{
		{name: "object", in: `{"a":1,"b":[2,3],"c":{"d":true},"e":null,"f":"s"}`},
		{name: "array", in: `[1,"two",false,null,[3],{"four":4}]`},
		{name: "string", in: `"hello"`},
		{name: "number forms", in: `[1.20,-0,1e+5,-1e-10,123456789012345678901234567890]`},
		{name: "empty object", in: `{}`, out: `{}`},
		{name: "empty array", in: `[]`, out: `[]`},
		{name: "astral string", in: `"𝄞 clef"`, out: `"𝄞 clef"`},
		{name: "escaped newline", in: `{"a":"one\ntwo"}`},
		{name: "uppercase exponent", in: "1E5", out: `1e5`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := cnfjson.ParseJSON([]byte(tc.in))
			if err != nil {
				t.Fatalf("parse error: %s", err)
			}
			cnfOut, err := cnfast.Render(root, nil)
			if err != nil {
				t.Fatalf("cnf render error: %s", err)
			}
			// The cnf re-parses: a parse failure here means
			// the cnf render emitted text its own grammar
			// rejects.
			reparsed, err := cnfast.Parse(cnfOut, nil)
			if err != nil {
				t.Fatalf("cnf does not re-parse %q: %s", cnfOut, err)
			}
			jsonOut, err := cnfjson.RenderJSON(reparsed)
			if err != nil {
				t.Fatalf("json render error: %s", err)
			}
			want := tc.out
			if want == "" {
				want = tc.in
			}
			if got := trimJSON(string(jsonOut)); got != trimJSON(want) {
				t.Errorf("round trip:\n got %q\nwant %q", got, trimJSON(want))
			}
		})
	}
}

// TestParseJSONErrors pins the refusals: duplicate keys
// rejected rather than last-won, trailing values rejected,
// malformed input surfacing the decoder's error.
func TestParseJSONErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string // a substring of the error
	}{
		{name: "duplicate key", in: `{"a":1,"a":2}`, want: `cannot redefine key "a"`},
		{name: "duplicate nested", in: `{"a":{"b":1,"b":2}}`, want: `cannot redefine key "b"`},
		{name: "trailing value", in: `{"a":1} {"b":2}`, want: "trailing"},
		{name: "two documents", in: `1 2`, want: "trailing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cnfjson.ParseJSON([]byte(tc.in))
			if err == nil {
				t.Fatalf("parsed without error, want %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
	// The malformed-input class: whatever the decoder says, it
	// is an error, not a panic.
	if _, err := cnfjson.ParseJSON([]byte(`{"a":`)); err == nil {
		t.Error("parsed truncated input without error")
	}
}

// TestFromJSON pins the cnf output of a few shapes against
// their canonical spellings: the fresh tree renders without
// any layout recorded, so the spellings are the render's
// defaults.
func TestFromJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "object", in: `{"a":1,"b":{"c":2}}`, want: "a: 1\nb:\n    c: 2\n"},
		{name: "array", in: `[1,2]`, want: "[1, 2]\n"},
		{name: "scalar", in: `true`, want: "true\n"},
		{name: "null", in: `null`, want: "null\n"},
		{name: "string", in: `"a string"`, want: `"a string"` + "\n"},
		{name: "empty object", in: `{}`, want: "{}\n"},
		{name: "nested empty object", in: `{"m":{}}`, want: "m: {}\n"},
		{name: "nested arrays", in: `{"l":[[1],[2,3]]}`, want: "l: [[1], [2, 3]]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := cnfjson.FromJSON([]byte(tc.in))
			if err != nil {
				t.Fatalf("error: %s", err)
			}
			if string(out) != tc.want {
				t.Errorf("cnf:\n got %q\nwant %q", out, tc.want)
			}
		})
	}
}
