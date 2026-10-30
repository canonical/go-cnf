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
	"reflect"
	"testing"

	"github.com/canonical/go-cnf/cnfmap"
)

// unmarshalTests is the unmarshal table: one cnf document
// in, the Go value out. The rows exercise the paths the
// corpus touches only thinly - the number bases, the big
// integers that degrade to doubles, the dotted-key merges
// and their conflicts.
var unmarshalTests = []struct {
	name string
	in   string
	want any
}{{
	name: "scalars",
	in:   "a: 1\nb: \"two\"\nc: true\nd: false\ne: null\n",
	want: map[string]any{"a": 1.0, "b": "two", "c": true, "d": false, "e": nil},
}, {
	name: "decimal number",
	in:   "a: 1.25\nb: -2\nc: 1e2\n",
	want: map[string]any{"a": 1.25, "b": -2.0, "c": 100.0},
}, {
	name: "hex number",
	in:   "a: 0x10\n",
	want: map[string]any{"a": 16.0},
}, {
	name: "octal number",
	in:   "a: 0o10\n",
	want: map[string]any{"a": 8.0},
}, {
	name: "binary number",
	in:   "a: 0b101\n",
	want: map[string]any{"a": 5.0},
}, {
	name: "hex float degrade",
	in:   "a: 0xffffffffffffffff\n",
	want: map[string]any{"a": 18446744073709551615.0},
}, {
	name: "inline list",
	in:   "a: [1, 2]\n",
	want: map[string]any{"a": []any{1.0, 2.0}},
}, {
	name: "block list",
	in:   "a:\n    - 1\n    - 2\n",
	want: map[string]any{"a": []any{1.0, 2.0}},
}, {
	name: "keyed list element",
	in:   "a:\n    - k: 1\n",
	want: map[string]any{"a": []any{map[string]any{"k": 1.0}}},
}, {
	name: "inline map",
	in:   "a: {b: 1}\n",
	want: map[string]any{"a": map[string]any{"b": 1.0}},
}, {
	name: "block map",
	in:   "a:\n    b: 1\n",
	want: map[string]any{"a": map[string]any{"b": 1.0}},
}, {
	name: "dotted keys merge",
	in:   "a.b: 1\na.c: 2\n",
	want: map[string]any{"a": map[string]any{"b": 1.0, "c": 2.0}},
}, {
	name: "dotted key against plain",
	in:   "a: 1\nb.c: 2\n",
	want: map[string]any{"a": 1.0, "b": map[string]any{"c": 2.0}},
}, {
	name: "map meets map merges",
	in:   "a.b: 1\na.c.d: 2\nx:\n    y: 3\na.e: 4\n",
	want: map[string]any{"a": map[string]any{"b": 1.0, "c": map[string]any{"d": 2.0}, "e": 4.0}, "x": map[string]any{"y": 3.0}},
}, {
	name: "top-level scalar",
	in:   "42\n",
	want: 42.0,
}, {
	name: "top-level string",
	in:   "\"two words\"\n",
	want: "two words",
}, {
	name: "quoted string",
	in:   "a: \"two words\"\n",
	want: map[string]any{"a": "two words"},
}, {
	name: "empty document",
	in:   "",
	want: map[string]any{},
}}

// unmarshalErrTests is the unmarshal error table: one cnf
// document in, the error text out.
var unmarshalErrTests = []struct {
	name string
	in   string
	want string
}{{
	name: "scalar against map",
	in:   "a: 1\na.b: 2\n",
	want: "2:1: cannot redefine previous key `a`",
}, {
	name: "map against scalar",
	in:   "a.b: 1\na: 2\n",
	want: "2:1: cannot redefine previous key `a`",
}, {
	name: "junk suffix on number",
	in:   "a: 1x\n",
	want: "1:4: cannot parse number \"1x\"",
}, {
	name: "empty hex digits",
	in:   "a: 0x\n",
	want: "1:4: cannot parse number \"0x\"",
}, {
	name: "bad hex digit",
	in:   "a: 0xg\n",
	want: "1:4: cannot parse number \"0xg\"",
}, {
	name: "bare word in value position",
	in:   "a: two\n",
	want: "1:4: cannot parse unquoted value \"two\"",
}}

func TestUnmarshal(t *testing.T) {
	for _, tc := range unmarshalTests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cnfmap.Unmarshal([]byte(tc.in))
			if err != nil {
				t.Fatalf("%s: %s", tc.name, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s:\n got  %#v\nwant %#v", tc.name, got, tc.want)
			}
		})
	}
	for _, tc := range unmarshalErrTests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cnfmap.Unmarshal([]byte(tc.in))
			if err == nil {
				t.Fatalf("%s: no error, want %q", tc.name, tc.want)
			}
			if err.Error() != tc.want {
				t.Errorf("%s:\n got  %q\nwant %q", tc.name, err.Error(), tc.want)
			}
		})
	}
}
