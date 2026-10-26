package cnfjson

import (
	"fmt"
	"strings"

	"github.com/canonical/now/internal/cnf/cnfast"
)

// writeNumber writes one number as JSON: the decimal
// forms pass through verbatim - 1.20 keeps its zeros, and
// integers too wide for a float64 keep their precision - and
// the other bases convert to decimal, any width, by repeated
// division of the digit string.
func writeNumber(b *strings.Builder, n *cnfast.Num) error {
	word := n.Text
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
			return fmt.Errorf("cannot parse number %q", word)
		}
		b.WriteString(out)
		return nil
	}
	b.WriteString(word)
	return nil
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
