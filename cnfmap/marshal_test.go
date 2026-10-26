package cnfmap_test

import (
	"math"
	"testing"

	"github.com/canonical/now/internal/cnf/cnfmap"
)

// marshalTests is the marshal table: one Go value in, the
// cnf text out. The rows exercise the kinds the corpus
// cannot reach - the from-json pipeline feeds the walk
// only what encoding/json makes, so the integer kinds, the
// pointers, the nil containers, and the cycles live here.
var marshalTests = []struct {
	name string
	in   any
	want string
}{{
	name: "bools",
	in:   map[string]any{"yes": true, "no": false},
	want: "no: false\nyes: true\n",
}, {
	name: "int kinds",
	in:   map[string]any{"i": int(-3), "i8": int8(8), "i64": int64(64), "u": uint(3), "u8": uint8(8), "u64": uint64(64)},
	want: "i: -3\ni64: 64\ni8: 8\nu: 3\nu64: 64\nu8: 8\n",
}, {
	name: "float kinds",
	in:   map[string]any{"f32": float32(1.5), "f64": 2.5},
	want: "f32: 1.5\nf64: 2.5\n",
}, {
	name: "float e-notation",
	in:   map[string]any{"big": 1e21},
	want: "big: 1e+21\n",
}, {
	name: "nil",
	in:   nil,
	want: "null\n",
}, {
	name: "nil any in list",
	in:   []any{nil, 1},
	want: "[null, 1]\n",
}, {
	name: "nested block map",
	in:   map[string]any{"outer": map[string]any{"inner": 1}},
	want: "outer:\n    inner: 1\n",
}, {
	name: "map inside list stays inline",
	in:   map[string]any{"l": []any{map[string]any{"a": 1}}},
	want: "l: [{a: 1}]\n",
}, {
	name: "empty map inline",
	in:   map[string]any{"empty": map[string]any{}},
	want: "empty: {}\n",
}, {
	name: "empty list",
	in:   map[string]any{"l": []any{}},
	want: "l: []\n",
}, {
	name: "top-level list",
	in:   []any{"a", 1},
	want: `["a", 1]` + "\n",
}, {
	name: "top-level string needs quotes",
	in:   "two words",
	want: "\"two words\"\n",
}, {
	name: "top-level number",
	in:   42,
	want: "42\n",
}, {
	name: "nil slice spells null",
	in:   map[string]any{"s": []int(nil)},
	want: "s: null\n",
}, {
	name: "nil map spells null",
	in:   map[string]any{"m": map[string]int(nil)},
	want: "m: null\n",
}, {
	name: "nil pointer spells null",
	in:   map[string]any{"p": (*int)(nil)},
	want: "p: null\n",
}, {
	name: "pointer is followed",
	in:   map[string]any{"p": &[]any{1}},
	want: "p: [1]\n",
}, {
	name: "array is a list",
	in:   [2]int{1, 2},
	want: "[1, 2]\n",
}, {
	name: "string keys sort",
	in:   map[string]any{"b": 2, "a": 1, "c": 3},
	want: "a: 1\nb: 2\nc: 3\n",
}, {
	name: "int map keys in decimal",
	in:   map[int]any{2: "b", 1: "a"},
	want: "1: \"a\"\n2: \"b\"\n",
}, {
	name: "interface followed",
	in:   map[string]any{"i": any(3)},
	want: "i: 3\n",
}}

// marshalErrTests is the marshal error table: one Go value
// in, the error text out.
var marshalErrTests = []struct {
	name string
	in   any
	want string
}{{
	name: "chan",
	in:   make(chan int),
	want: "cannot encode chan int",
}, {
	name: "func",
	in:   func() {},
	want: "cannot encode func()",
}, {
	name: "struct",
	in:   struct{ A int }{1},
	want: "cannot encode struct { A int }",
}, {
	name: "map key array",
	in:   map[[2]int]any{{1, 2}: "v"},
	want: "cannot encode map key of type [2]int",
}, {
	name: "float NaN",
	in:   math.NaN(),
	want: "cannot encode NaN or infinity as a cnf number",
}, {
	name: "float32 inf",
	in:   float32(math.Inf(1)),
	want: "cannot encode NaN or infinity as a cnf number",
}, {
	name: "self-referencing slice",
	in:   cyclicSlice(),
	want: "cannot encode cyclic value of type []interface {}",
}, {
	name: "self-referencing map",
	in:   cyclicMap(),
	want: "cannot encode cyclic value of type map[string]interface {}",
}, {
	name: "self-referencing pointer",
	in:   cyclicPtr(),
	want: "cannot encode cnfmap_test.box",
}}

type box struct{ Self *box }

func cyclicSlice() any {
	s := make([]any, 1)
	s[0] = s
	return s
}

func cyclicMap() any {
	m := map[string]any{}
	m["self"] = m
	return m
}

func cyclicPtr() any {
	b := &box{}
	b.Self = b
	return b
}

func TestMarshal(t *testing.T) {
	for _, tc := range marshalTests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cnfmap.Marshal(tc.in)
			if err != nil {
				t.Fatalf("%s: %s", tc.name, err)
			}
			if string(got) != tc.want {
				t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
			}
		})
	}
	for _, tc := range marshalErrTests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cnfmap.Marshal(tc.in)
			if err == nil {
				t.Fatalf("%s: no error, want %q", tc.name, tc.want)
			}
			if err.Error() != tc.want {
				t.Errorf("%s:\n got  %q\nwant %q", tc.name, err.Error(), tc.want)
			}
		})
	}
}
