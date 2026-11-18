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
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/canonical/go-cnf/cnfembed"
	"github.com/canonical/go-cnf/cnfjson"
	"github.com/canonical/go-cnf/cnftest"
)

// TestDifferential runs every cnf block of the corpus through
// both cnfjson and cnfembed, requiring byte-identical json
// and byte-identical error messages - a stricter check than
// the conformance harness, whose assertion trims whitespace
// and only advises on some kinds.
func TestDifferential(t *testing.T) {
	topics, err := cnftest.ReadAll("../spec/cases")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, topic := range topics {
		for _, c := range topic.Cases {
			for i, block := range c.CNF {
				n++
				name := fmt.Sprintf("%s/%s/cnf#%d", topic.Name, c.Name, i+1)
				want, werr := cnfjson.ToJSON([]byte(block), nil)
				got, gerr := cnfembed.ToJSON([]byte(block), nil)
				if (werr == nil) != (gerr == nil) {
					t.Errorf("%s: error mismatch:\n cnfjson=%v\n cnfembed=%v", name, werr, gerr)
					continue
				}
				if werr != nil {
					if werr.Error() != gerr.Error() {
						t.Errorf("%s: error text mismatch:\n cnfjson=%q\n cnfembed=%q", name, werr.Error(), gerr.Error())
					}
					continue
				}
				if string(want) != string(got) {
					t.Errorf("%s: json mismatch:\nwant %s\n got %s", name, want, got)
				}
			}
		}
	}
	t.Log("blocks checked:", n)
}

// TestWideBaseNumber pins the conversion of a wide non-decimal
// number in both implementations: the value is exact, and the
// time stays far below the seconds a quadratic conversion takes
// at this width.
func TestWideBaseNumber(t *testing.T) {
	const zeros = 1 << 16
	doc := []byte("a: 0x1" + strings.Repeat("0", zeros) + "\n")
	want := `{"a":` + new(big.Int).Lsh(big.NewInt(1), 4*zeros).String() + `}`
	for name, toJSON := range map[string]func([]byte) ([]byte, error){
		"cnfembed": func(d []byte) ([]byte, error) { return cnfembed.ToJSON(d, nil) },
		"cnfjson":  func(d []byte) ([]byte, error) { return cnfjson.ToJSON(d, nil) },
	} {
		start := time.Now()
		got, err := toJSON(doc)
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("%s: took %s", name, elapsed)
		}
		if err != nil || string(got) != want {
			t.Errorf("%s: wrong value, err %v", name, err)
		}
	}
}
