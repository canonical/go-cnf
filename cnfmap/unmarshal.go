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
	"strconv"
	"strings"

	"github.com/canonical/go-cnf/cnfast"
)

// Unmarshal decodes cnf text into a Go value: `float64`, `string`, `bool`,
// `nil`, `[]any`, or `map[string]any`.
func Unmarshal(data []byte) (any, error) {
	item, err := cnfast.Parse(data, nil)
	if err != nil {
		return nil, err
	}
	return value(item.Value)
}

// value reads one value node as a plain Go value.
func value(v cnfast.Value) (any, error) {
	switch n := v.(type) {
	case *cnfast.Str:
		return n.Data, nil
	case *cnfast.Scalar:
		switch n.Text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		case "null":
			return nil, nil
		}
		return nil, fmt.Errorf("cannot decode scalar %q", n.Text)
	case *cnfast.Num:
		return parseNumber(n.Text)
	case *cnfast.List:
		items := make([]any, 0, len(n.Items))
		for i := range n.Items {
			elem := n.Items[i]
			// A keyed list element ("- label: first map")
			// reads as the one-pair map its pair spells: the
			// key wrapping the value the way a map key does.
			if len(elem.Key) > 0 {
				val, err := value(elem.Value)
				if err != nil {
					return nil, err
				}
				m := map[string]any{}
				if err := setPath(m, keyPath(elem), val); err != nil {
					return nil, err
				}
				// A bare key path with one segment sets the
				// single pair; the wrap above covers the rest.
				items = append(items, m)
				continue
			}
			val, err := value(elem.Value)
			if err != nil {
				return nil, err
			}
			items = append(items, val)
		}
		return items, nil
	case *cnfast.Map:
		m := map[string]any{}
		for _, item := range n.Items {
			val, err := value(item.Value)
			if err != nil {
				return nil, err
			}
			if err := setPath(m, keyPath(item), val); err != nil {
				// The document is the wrong side here: the position
				// names the pair that cannot join the tree, the key
				// spelled on the pair's own line.
				pos := item.Key[0].Pos
				return nil, fmt.Errorf("%d:%d: %s", pos.L, pos.C, err)
			}
		}
		return m, nil
	}
	return nil, fmt.Errorf("cannot decode %T", v)
}

// keyPath reads one item's key segments as a plain string path.
func keyPath(item *cnfast.Item) []string {
	path := make([]string, len(item.Key))
	for i, seg := range item.Key {
		path[i] = seg.Data
	}
	return path
}

// parseNumber reads the cnf number spellings. All numbers are
// IEEE754 doubles underneath. Decimal forms parse through
// strconv; the other bases convert digit by digit, integers
// within int64 keeping their exact value and wider ones
// degrading to the nearest double rather than failing - the
// spec's floor is double precision, and a small decoder meets
// it without big-number machinery.
func parseNumber(s string) (float64, error) {
	sign := float64(1)
	text := s
	if len(s) > 0 && s[0] == '-' {
		sign, s = -1, s[1:]
	}
	base := 10
	digits := s
	switch {
	case strings.HasPrefix(s, "0x"):
		base, digits = 16, s[2:]
	case strings.HasPrefix(s, "0o"):
		base, digits = 8, s[2:]
	case strings.HasPrefix(s, "0b"):
		base, digits = 2, s[2:]
	}
	if base != 10 {
		// Integers within int64 keep their exact value; wider
		// ones degrade to the nearest float64 rather than
		// failing - the spec's floor is double precision, and
		// a tiny parser meets it without big-number machinery.
		if digits == "" {
			return 0, fmt.Errorf("cannot parse number %q", text)
		}
		if n, err := strconv.ParseInt(digits, base, 64); err == nil {
			return sign * float64(n), nil
		}
		if n, err := strconv.ParseUint(digits, base, 64); err == nil {
			return sign * float64(n), nil
		}
		var f float64
		for _, c := range digits {
			d := digitVal(c)
			if d < 0 || d >= base {
				return 0, fmt.Errorf("cannot parse number %q", text)
			}
			f = f*float64(base) + float64(d)
		}
		return sign * f, nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot decode number: %s is out of range for float64", text)
	}
	return sign * n, nil
}

// digitVal reads one digit of any base up to 16, or -1 when the
// character is not a digit.
func digitVal(c rune) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return -1
}

// setPath merges val into m at the dotted path. Map meets map
// merges recursively; anything else at the same path is a
// conflict: the document spelled the same key twice, and the
// message names the pair that cannot join the one already there.
func setPath(m map[string]any, path []string, val any) error {
	for i, seg := range path {
		last := i == len(path)-1
		existing, exists := m[seg]
		if last {
			if !exists {
				m[seg] = val
				return nil
			}
			em, ok1 := existing.(map[string]any)
			vm, ok2 := val.(map[string]any)
			if ok1 && ok2 {
				return mergeMaps(em, vm)
			}
			return fmt.Errorf("cannot merge with previous `%s`: not a map", seg)
		}
		if !exists {
			nested := map[string]any{}
			m[seg] = nested
			existing = nested
		}
		subMap, ok := existing.(map[string]any)
		if !ok {
			return fmt.Errorf("cannot expand key %q into a map", seg)
		}
		m = subMap
	}
	return nil
}

func mergeMaps(dst, src map[string]any) error {
	for k, v := range src {
		if d, exists := dst[k]; exists {
			dm, ok1 := d.(map[string]any)
			vm, ok2 := v.(map[string]any)
			if ok1 && ok2 {
				if err := mergeMaps(dm, vm); err != nil {
					return err
				}
				continue
			}
			return fmt.Errorf("cannot merge with previous `%s`: not a map", k)
		}
		dst[k] = v
	}
	return nil
}
