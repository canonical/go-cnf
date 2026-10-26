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

package cnfmap

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"

	"github.com/canonical/go-cnf/cnfast"
)

// pairIndent is the indent a pair records when it holds a
// block value: the writer's fixed one-level step, the same
// one the render falls back on when a tree arrives with no
// indent recorded.
const pairIndent = "    "

// Marshal encodes one Go value as a cnf document. The walk
// is reflection-based, so every shape Go offers is read
// structurally:
//
//   - nil, a nil pointer, a nil slice, and a nil map spell
//     null.
//   - bool, string, and every integer and float kind spell
//     themselves. A float NaN or infinity is an error: cnf
//     has no spelling for either.
//   - a slice or an array is a list.
//   - a map is a map. The keys must be strings or integers.
//     The pairs are written in sorted key order.
//   - a pointer or an interface is followed.
//
// .
func Marshal(v any) ([]byte, error) {
	b := &builder{onPath: map[uintptr]bool{}}
	root, err := b.value(reflect.ValueOf(v), false)
	if err != nil {
		return nil, err
	}
	return cnfast.Render(&cnfast.Item{Value: root}, nil)
}

// A builder walks one value at a time. onPath holds the
// reference targets on the current path, so a value that
// reaches itself is caught instead of recursing forever.
type builder struct {
	onPath map[uintptr]bool
}

// value builds one ast value node. The inline flag carries
// the context: a list holds only inline values, so everything
// under one inlines - the same rule cnfast's render enforces.
// An empty map stays inline everywhere: a block map with no
// pairs has no cnf spelling, so the inline {} is the only
// form left for it.
func (b *builder) value(rv reflect.Value, inline bool) (cnfast.Value, error) {
	if !rv.IsValid() {
		// A nil any carries no type at all.
		return &cnfast.Scalar{Text: "null"}, nil
	}
	switch rv.Kind() {
	case reflect.Interface:
		if rv.IsNil() {
			return &cnfast.Scalar{Text: "null"}, nil
		}
		return b.value(rv.Elem(), inline)
	case reflect.Ptr:
		if rv.IsNil() {
			return &cnfast.Scalar{Text: "null"}, nil
		}
		if err := b.enter(rv); err != nil {
			return nil, err
		}
		defer b.leave(rv)
		return b.value(rv.Elem(), inline)
	case reflect.Bool:
		if rv.Bool() {
			return &cnfast.Scalar{Text: "true"}, nil
		}
		return &cnfast.Scalar{Text: "false"}, nil
	case reflect.String:
		// Unquoted escalates at render time.
		return &cnfast.Str{Data: rv.String(), Quote: cnfast.Unquoted}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &cnfast.Num{Text: strconv.FormatInt(rv.Int(), 10)}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &cnfast.Num{Text: strconv.FormatUint(rv.Uint(), 10)}, nil
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("cannot encode NaN or infinity as a cnf number")
		}
		bits := 64
		if rv.Kind() == reflect.Float32 {
			bits = 32
		}
		return &cnfast.Num{Text: strconv.FormatFloat(f, 'g', -1, bits)}, nil
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return &cnfast.Scalar{Text: "null"}, nil
		}
		if err := b.enter(rv); err != nil {
			return nil, err
		}
		defer b.leave(rv)
		items := make([]*cnfast.Item, rv.Len())
		for i := range items {
			av, err := b.value(rv.Index(i), true)
			if err != nil {
				return nil, err
			}
			items[i] = &cnfast.Item{Value: av}
		}
		return &cnfast.List{Items: items}, nil
	case reflect.Map:
		if rv.IsNil() {
			return &cnfast.Scalar{Text: "null"}, nil
		}
		if err := b.enter(rv); err != nil {
			return nil, err
		}
		defer b.leave(rv)
		keys := make([]string, 0, rv.Len())
		texts := make(map[string]reflect.Value, rv.Len())
		for _, k := range rv.MapKeys() {
			text, err := keyText(k)
			if err != nil {
				return nil, err
			}
			keys = append(keys, text)
			texts[text] = k
		}
		sort.Strings(keys)
		pairs := make([]*cnfast.Item, 0, len(keys))
		for _, text := range keys {
			av, err := b.value(rv.MapIndex(texts[text]), inline)
			if err != nil {
				return nil, err
			}
			pair := &cnfast.Item{
				Key:   []*cnfast.Str{{Data: text, Quote: cnfast.Unquoted}},
				Value: av,
			}
			if !inline {
				if m, ok := av.(*cnfast.Map); ok && m.Block {
					pair.Indent = pairIndent
				}
			}
			pairs = append(pairs, pair)
		}
		return &cnfast.Map{Items: pairs, Block: !inline && len(pairs) > 0}, nil
	}
	return nil, fmt.Errorf("cannot encode %s", rv.Type())
}

// keyText reads one map key as a pair name: a string key
// as it is, an integer key in decimal. Any other key type
// is an error.
func keyText(k reflect.Value) (string, error) {
	switch k.Kind() {
	case reflect.String:
		return k.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(k.Uint(), 10), nil
	}
	return "", fmt.Errorf("cannot encode map key of type %s", k.Type())
}

// enter marks a reference target as on the current path,
// failing when it is already there: the walk would loop
// forever through the cycle it closes. An array holds no
// reference of its own, so it takes no marking - it cannot
// close a cycle.
func (b *builder) enter(rv reflect.Value) error {
	if rv.Kind() == reflect.Array {
		return nil
	}
	p := rv.Pointer()
	if p == 0 {
		return nil
	}
	if b.onPath[p] {
		return fmt.Errorf("cannot encode cyclic value of type %s", rv.Type())
	}
	b.onPath[p] = true
	return nil
}

// leave takes a reference target off the current path. An
// array never entered it.
func (b *builder) leave(rv reflect.Value) {
	if rv.Kind() == reflect.Array {
		return
	}
	p := rv.Pointer()
	if p != 0 {
		delete(b.onPath, p)
	}
}
