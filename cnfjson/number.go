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

package cnfjson

import (
	"fmt"
	"strings"

	"github.com/canonical/go-cnf/cnfast"
)

// writeNumber writes one number as JSON: the decimal
// forms pass through verbatim - 1.20 keeps its zeros, and
// integers too wide for a float64 keep their precision - and
// the other bases convert to decimal, any width, by repeated
// division of the digit string.
func writeNumber(b *strings.Builder, n *cnfast.Num) error {
	word := n.Text
	sign := ""
	if len(word) > 0 && word[0] == '-' {
		sign, word = "-", word[1:]
	}
	base := 10
	digits := word
	switch {
	case strings.HasPrefix(word, "0x"):
		base, digits = 16, word[2:]
	case strings.HasPrefix(word, "0o"):
		base, digits = 8, word[2:]
	case strings.HasPrefix(word, "0b"):
		base, digits = 2, word[2:]
	}
	if base != 10 {
		out, err := baseToDecimal(digits, base)
		if err != nil {
			return fmt.Errorf("cannot parse number %q", n.Text)
		}
		b.WriteString(sign)
		b.WriteString(out)
		return nil
	}
	if !isDecimalShape(word) {
		return fmt.Errorf("cannot parse number %q", n.Text)
	}
	b.WriteString(n.Text)
	return nil
}

// isDecimalShape reports whether body matches the grammar's
// decimal spellings, mirroring cnfast's own check: numint
// ["." 1*DIGIT] ["e" ["+" / "-"] 1*DIGIT].
func isDecimalShape(body string) bool {
	mantissa := body
	if i := strings.IndexByte(body, 'e'); i >= 0 {
		mantissa = body[:i]
		exp := body[i+1:]
		if exp == "" {
			return false
		}
		if exp[0] == '+' || exp[0] == '-' {
			exp = exp[1:]
		}
		if !allDigits(exp) {
			return false
		}
	}
	intPart := mantissa
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		intPart = mantissa[:i]
		if !allDigits(mantissa[i+1:]) {
			return false
		}
	}
	return intPart != "" && allDigits(intPart)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// baseToDecimal converts a digit string in the given base to
// its decimal form, by repeated division by 10: each pass
// walks the digits once, most significant first, carrying the
// remainder - one decimal digit out per pass, least
// significant first.
func baseToDecimal(digits string, base int) (string, error) {
	if digits == "" {
		return "", fmt.Errorf("no digits")
	}
	for _, c := range digits {
		if _, ok := digitVal(c, base); !ok {
			return "", fmt.Errorf("bad digit %q", c)
		}
	}
	var out []byte
	for digits != "" {
		carry := 0
		var quot []byte
		for _, c := range digits {
			v, _ := digitVal(c, base)
			cur := carry*base + v
			q := cur / 10
			if q < 10 {
				quot = append(quot, '0'+byte(q))
			} else {
				quot = append(quot, 'a'+byte(q-10))
			}
			carry = cur % 10
		}
		// Strip the leading zeros the quotient accumulated; an
		// empty result means the value hit zero and the loop
		// ends.
		i := 0
		for i < len(quot) && quot[i] == '0' {
			i++
		}
		digits = string(quot[i:])
		out = append(out, '0'+byte(carry))
	}
	// The digits came out least significant first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}

// digitVal reads one digit of the given base, reporting
// whether it is valid.
func digitVal(c rune, base int) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		v := int(c - '0')
		return v, v < base
	case c >= 'a' && c <= 'z':
		v := int(c-'a') + 10
		return v, v < base
	case c >= 'A' && c <= 'F':
		v := int(c-'A') + 10
		return v, v < base
	}
	return 0, false
}
