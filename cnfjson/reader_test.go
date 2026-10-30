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

// TestRenderJSONAST pins RenderJSON against the AST the text
// paths never produce: a Num whose text the cnf parser would
// have rejected. The parser gates the text-in directions - a
// bad number never reaches the render - but the AST-in path
// has no gate: cnfast.UnmarshalJSON decodes a num verbatim,
// and RenderJSON is a public API taking any tree. The bad
// digit fails the render, not silently passing through.
func TestRenderJSONAST(t *testing.T) {
	t.Run("bad digit", func(t *testing.T) {
		root := item(&cnfast.Map{Items: []*cnfast.Item{{
			Key:   []*cnfast.Str{{Data: "a"}},
			Value: &cnfast.Num{Text: "12b"},
		}}})
		_, err := cnfjson.RenderJSON(root, nil)
		if err == nil {
			t.Fatalf("no error, want %q", `cannot parse number "12b"`)
		}
		want := `cannot parse number "12b"`
		if err.Error() != want {
			t.Fatalf("error:\n  got  %q\n  want %q", err.Error(), want)
		}
	})
	t.Run("signed base", func(t *testing.T) {
		// The sign-split path of writeNumber, reached from the
		// AST alone: the based forms convert to their signed
		// decimal value.
		root := item(&cnfast.Map{Items: []*cnfast.Item{{
			Key:   []*cnfast.Str{{Data: "a"}},
			Value: &cnfast.Num{Text: "-0x1f"},
		}}})
		out, err := cnfjson.RenderJSON(root, nil)
		if err != nil {
			t.Fatalf("render error: %s", err)
		}
		if got := string(out); got != `{"a":-31}` {
			t.Fatalf("render:\n  got  %q\n  want %q", got, `{"a":-31}`)
		}
	})
}

// item wraps one value in the root Item RenderJSON walks.
func item(v cnfast.Value) *cnfast.Item {
	return &cnfast.Item{Value: v}
}
