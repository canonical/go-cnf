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
	"testing"

	"github.com/canonical/go-cnf/cnfast"
	"github.com/canonical/go-cnf/cnfjson"
)

// renderJSONTests is the RenderJSON table against the AST
// the text paths never produce, nor validate: a hand-built
// tree reaches the render unchanged by the parser's gates -
// its digit grammar, its conflict check - so both the
// render's own checks and the keyTree's redefinition rule
// are pinned here, error rows included: the message the
// tree emits, positioned when the AST carries a position
// and bare when it does not, matched verbatim.
var renderJSONTests = []struct {
	name  string
	value cnfast.Value // the root's value
	want  string       // the JSON out, "" when the row wants the error
	err   string       // the error text, "" when the row wants the JSON
}{{
	name:  "number verbatim",
	value: &cnfast.Map{Items: []*cnfast.Item{pair("a", num("1.20"))}},
	want:  `{"a":1.20}`,
}, {
	name:  "signed base",
	value: &cnfast.Map{Items: []*cnfast.Item{pair("a", num("-0x1f"))}},
	want:  `{"a":-31}`,
}, {
	name:  "path dissolved",
	value: &cnfast.Map{Items: []*cnfast.Item{pathOf(num("1"), "a", "b")}},
	want:  `{"a":{"b":1}}`,
}, {
	name:  "bad digit",
	value: &cnfast.Map{Items: []*cnfast.Item{pair("a", num("12b"))}},
	err:   `cannot parse number "12b"`,
}, {
	name: "scalar over scalar",
	value: &cnfast.Map{Items: []*cnfast.Item{
		pair("a", num("1")),
		pair("a", num("2")),
	}},
	err: "cannot redefine previous key `a`",
}, {
	name: "map over scalar",
	value: &cnfast.Map{Items: []*cnfast.Item{
		pair("a", num("1")),
		pair("a", &cnfast.Map{}),
	}},
	err: "cannot redefine previous key `a`",
}, {
	name: "path through scalar",
	value: &cnfast.Map{Items: []*cnfast.Item{
		pair("a", num("1")),
		pathOf(num("1"), "a", "b"),
	}},
	err: "cannot redefine previous key `a`",
}, {
	name: "scalar over traversed",
	value: &cnfast.Map{Items: []*cnfast.Item{
		pathOf(num("1"), "a", "b"),
		pair("a", num("2")),
	}},
	err: "cannot redefine previous key `a`",
}, {
	name: "redefined positioned",
	value: &cnfast.Map{Items: []*cnfast.Item{
		pair("a", num("1")),
		at(2, 1, pair("a", num("2"))),
	}},
	err: "2:1: cannot redefine previous key `a`",
}}

// TestRenderJSON pins RenderJSON against the ASTs the text
// paths never produce: rows render to their JSON, or fail
// with the exact message the row names - the render's own
// gates, and the keyTree's redefinition rule, both doors
// the parser does not stand at.
func TestRenderJSON(t *testing.T) {
	for _, tc := range renderJSONTests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cnfjson.RenderJSON(&cnfast.Item{Value: tc.value}, nil)
			if tc.err != "" {
				if err == nil {
					t.Fatalf("no error, want %q", tc.err)
				}
				if err.Error() != tc.err {
					t.Fatalf("error:\n  got  %q\n  want %q", err.Error(), tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("render error: %s", err)
			}
			if string(got) != tc.want {
				t.Fatalf("render:\n  got  %q\n  want %q", got, tc.want)
			}
		})
	}
}

// pair builds one single-segment pair item.
func pair(k string, v cnfast.Value) *cnfast.Item {
	return &cnfast.Item{Key: []*cnfast.Str{{Data: k}}, Value: v}
}

// pathOf builds one multi-segment path item, the value at
// the path's end.
func pathOf(v cnfast.Value, segs ...string) *cnfast.Item {
	key := make([]*cnfast.Str, len(segs))
	for i, s := range segs {
		key[i] = &cnfast.Str{Data: s}
	}
	return &cnfast.Item{Key: key, Value: v}
}

// num builds one number value.
func num(text string) cnfast.Value {
	return &cnfast.Num{Text: text}
}

// at positions one item's first key segment: the hand-built
// tree's one source fact, the position an item's message
// prefixes when the render reports it.
func at(line, col int, it *cnfast.Item) *cnfast.Item {
	it.Key[0].Pos = cnfast.Pos{L: line, C: col}
	return it
}
