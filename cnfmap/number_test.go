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

package cnfmap_test

import (
	"math"
	"testing"

	"github.com/canonical/go-cnf/cnfmap"
)

// TestUnmarshalFloat64Range pins the decode of the decimal
// spellings at the float64 limit: the last in-range values
// decode - ParseFloat rounds to nearest, so a spelling up
// to half an ULP above the max still lands on it - and the
// values past it fail on the verbatim number text, sign
// included - the one decode error the parser-gated text
// input can still reach. The errors carry no position:
// parseNumber holds none. Underflow degrades to zero,
// the subnormal band and below included.
var unmarshalRangeTests = []struct {
	in   string
	want any // float64 to decode, string for the error text
}{{
	in:   "a: 1e308\n",
	want: 1e308,
}, {
	in:   "a: 1.7976931348623157e308\n",
	want: math.MaxFloat64,
}, {
	in:   "a: 1e-320\n",
	want: 1e-320,
}, {
	in:   "a: 1e-999\n",
	want: 0.0,
}, {
	in:   "a: 1.8e308\n",
	want: `cannot decode number: 1.8e308 is out of range for float64`,
}, {
	in:   "a: -1.8e308\n",
	want: `cannot decode number: -1.8e308 is out of range for float64`,
}, {
	in:   "a: 1e309\n",
	want: `cannot decode number: 1e309 is out of range for float64`,
}, {
	in:   "a: -1e309\n",
	want: `cannot decode number: -1e309 is out of range for float64`,
}}

func TestUnmarshalRange(t *testing.T) {
	for _, tc := range unmarshalRangeTests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := cnfmap.Unmarshal([]byte(tc.in))
			switch want := tc.want.(type) {
			case float64:
				if err != nil {
					t.Fatalf("unexpected error: %s", err)
				}
				m, ok := got.(map[string]any)
				if !ok {
					t.Fatalf("decoded %#v, want a map", got)
				}
				if m["a"] != want {
					t.Fatalf("decoded %v, want %v", m["a"], want)
				}
			case string:
				if err == nil {
					t.Fatalf("no error, want %q", want)
				}
				if err.Error() != want {
					t.Fatalf("error:\n  got  %q\n  want %q", err.Error(), want)
				}
			}
		})
	}
}
