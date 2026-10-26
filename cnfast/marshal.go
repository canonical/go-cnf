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

package cnfast

import (
	"encoding/json"
	"strings"
)

// inlineWidth is the threshold of the review form: a value
// whose compact form fits within this many columns renders on
// one line; anything wider breaks one field per line.
const inlineWidth = 40

// MarshalJSON renders v in the compact review form: objects
// multi-line with one field per line, arrays inline with their
// brackets and commas on one line - and any nested value small
// enough to fit the inline width compacted to a single line, so
// positions read as {"line":1,"col":1}.
func MarshalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	render(&b, string(raw), 0)
	return []byte(b.String()), nil
}

// render writes the compact JSON in the review form at the given
// indent level.
func render(b *strings.Builder, raw string, indent int) {
	i := 0
	pad := strings.Repeat("    ", indent)
	for i < len(raw) {
		c := raw[i]
		switch c {
		case '"':
			j := skipString(raw, i)
			b.WriteString(raw[i:j])
			i = j
		case '{', '[':
			close := findClose(raw, i)
			inner := raw[i+1 : close-1]
			// A value small enough to fit the width stays on
			// one line, compact.
			if len(inner) <= inlineWidth && fitsLine(inner) {
				b.WriteString(raw[i:close])
				i = close
				continue
			}
			if raw[i] == '{' {
				b.WriteString("{\n")
				fields := splitFields(inner)
				for k, f := range fields {
					b.WriteString(pad)
					b.WriteString("    ")
					renderField(b, f, indent+1)
					if k < len(fields)-1 {
						b.WriteString(",")
					}
					b.WriteString("\n")
				}
				b.WriteString(pad)
				b.WriteString("}")
			} else {
				b.WriteString("[")
				elems := splitFields(inner)
				for k, el := range elems {
					if k > 0 {
						b.WriteString(", ")
					}
					render(b, el, indent)
				}
				b.WriteString("]")
			}
			i = close
		default:
			b.WriteByte(c)
			i++
		}
	}
}

// fitsLine reports whether the raw content can render on one
// line: no newline-hostile shapes. The compact form never
// holds newlines, so this is a placeholder for future
// heuristics - for now, everything that fits the width
// renders inline.
func fitsLine(inner string) bool {
	return true
}

// renderField writes one object field - the "key": value pair -
// with a space after the colon, the value in review form.
func renderField(b *strings.Builder, field string, indent int) {
	colon := strings.Index(field, ":")
	b.WriteString(field[:colon+1])
	b.WriteString(" ")
	render(b, field[colon+1:], indent)
}

// skipString returns the index just past the string starting at
// i, escapes included.
func skipString(raw string, i int) int {
	j := i + 1
	for j < len(raw) {
		switch raw[j] {
		case '\\':
			j += 2
		case '"':
			return j + 1
		default:
			j++
		}
	}
	return j
}

// findClose returns the index just past the bracket opening at
// i, strings skipped.
func findClose(raw string, i int) int {
	depth := 0
	for j := i; j < len(raw); j++ {
		switch raw[j] {
		case '"':
			j = skipString(raw, j) - 1
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return j + 1
			}
		}
	}
	return len(raw)
}

// splitFields splits the raw content of an object or array into
// its top-level elements: fields for objects (the key and colon
// stay attached), elements for arrays.
func splitFields(inner string) []string {
	var out []string
	depth := 0
	start := 0
	i := 0
	for i < len(inner) {
		switch inner[i] {
		case '"':
			i = skipString(inner, i)
			continue
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, inner[start:i])
				start = i + 1
			}
		}
		i++
	}
	if start < len(inner) {
		out = append(out, inner[start:])
	}
	return out
}
