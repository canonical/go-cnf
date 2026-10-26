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

package cnftest

import (
	"fmt"
	"regexp"
	"strings"
)

// A Data value is one expected text a case carries, and
// how to read it: a literal, or a regexp - the kind picked
// by the value's shape in the case file. A quoted string in
// the header is a literal, matched and pinned as plain text.
// A single-key map with the key regexp - {regexp: "..."} - is
// a pattern, compiled and matched as a regexp, its first
// group pinning a position when it has one.
type Data struct {
	Text   string
	Regexp bool
}

// varRef finds one ${name} or ${name:form} reference in a
// target text, the name and the form each a capture group.
// The name is [a-z]+, the form lowercase letters; every
// reference ExpandVars resolves matches this.
var varRef = regexp.MustCompile(`\$\{([a-z]+)(?::([a-z]+))?\}`)

// ExpandVars substitutes a case's variables into a target
// text: the expected message of an error direction, an
// expected block, or an input block of a direction. Every
// reference is found by one scan, a reference being
// ${name} - the var's text - or ${name:l}, ${name:c},
// ${name:lc} - its located position, the forms resolved by
// an inline callback: the plain form takes the var's text
// directly, the position forms locate it in the source text
// - a literal's first occurrence, a regexp's first match
// anywhere in the block (multiline), its first group
// pinning the position.
//
// A reference to an undefined name is a broken case.
func ExpandVars(c *Case, source, target string) (string, error) {
	if !strings.Contains(target, "${") {
		return target, nil
	}
	// One scan finds every reference; each is resolved against
	// the case's vars, the groups read from the match. A name
	// that matches no var, an unknown form, or a locator that
	// finds nothing is collected into the error return rather
	// than thrown - the message names the first offender, the
	// scan still visiting the rest.
	var broken error
	resolve := func(ref, name, form string) string {
		data, ok := c.Vars[name]
		if !ok {
			if broken == nil {
				broken = fmt.Errorf("reference %s is not a defined var", ref)
			}
			return ref
		}
		if form == "" && !data.Regexp {
			return data.Text
		}
		text, line, col, err := locate(source, data)
		if err != nil {
			if broken == nil {
				broken = err
			}
			return ref
		}
		switch form {
		case "l":
			return fmt.Sprint(line)
		case "c":
			return fmt.Sprint(col)
		case "lc":
			return fmt.Sprintf("%d:%d", line, col)
		case "":
			return text
		default:
			if broken == nil {
				broken = fmt.Errorf("reference %s has an unknown form", ref)
			}
			return ref
		}
	}
	var b strings.Builder
	prev := 0
	for _, m := range varRef.FindAllStringSubmatchIndex(target, -1) {
		ref, form := target[m[0]:m[1]], ""
		if m[4] >= 0 {
			form = target[m[4]:m[5]]
		}
		b.WriteString(target[prev:m[0]])
		b.WriteString(resolve(ref, target[m[2]:m[3]], form))
		prev = m[1]
	}
	b.WriteString(target[prev:])
	out := b.String()
	if broken != nil {
		return "", broken
	}
	// Any ${...} left over - a reference whose name part is
	// not [a-z]+ - is undefined by the same rule.
	if i := strings.Index(out, "${"); i >= 0 {
		rest := out[i:]
		end := strings.Index(rest, "}")
		ref := rest
		if end >= 0 {
			ref = rest[:end+1]
		}
		return "", fmt.Errorf("reference %s is not a defined var", ref)
	}
	return out, nil
}

// MatchError checks an implementation's error message against a
// case's error expectation. The expectation is the error field
// of an error directive: a literal for exact comparison, or a
// regexp matched anywhere in the message - the pattern anchors
// itself if it wants to. It may reference the case's vars,
// resolved against the input block.
//
// The boolean reports whether the message satisfies the
// expectation. The error reports a broken case: an undefined
// reference or a locator that matches nothing.
func MatchError(c *Case, block string, err error) (bool, error) {
	want, xerr := ExpandVars(c, block, c.Error.Text)
	if xerr != nil {
		return false, xerr
	}
	msg := err.Error()
	if !c.Error.Regexp {
		return msg == want, nil
	}
	re, rerr := regexp.Compile(want)
	if rerr != nil {
		return false, fmt.Errorf("bad error regexp: %s", rerr)
	}
	return re.MatchString(msg), nil
}

// locate finds the expectation in the source text and returns
// the text, 1-based line, and rune column of the pin: a
// literal is pinned at its first occurrence, its whole self
// the pin; a regexp matches anywhere in the block, its first
// group the pin - the whole match when the pattern has no
// group. The regexp is multiline: ^ and $ match at line
// boundaries.
func locate(source string, data Data) (text string, line, col int, err error) {
	var start, end int
	if !data.Regexp {
		start = strings.Index(source, data.Text)
		if start < 0 {
			return "", 0, 0, fmt.Errorf("literal %q matches nothing in the block", data.Text)
		}
		end = start + len(data.Text)
	} else {
		re, cerr := regexp.Compile("(?m)" + data.Text)
		if cerr != nil {
			return "", 0, 0, fmt.Errorf("bad locator regexp: %s", cerr)
		}
		loc := re.FindStringSubmatchIndex(source)
		if loc == nil {
			return "", 0, 0, fmt.Errorf("locator regexp %q matches nothing in the block", data.Text)
		}
		start, end = loc[0], loc[1]
		if len(loc) >= 4 && loc[2] >= 0 {
			start, end = loc[2], loc[3]
		}
	}
	before := source[:start]
	line = strings.Count(before, "\n") + 1
	lineStart := strings.LastIndex(before, "\n") + 1
	col = len([]rune(source[lineStart:start])) + 1
	return source[start:end], line, col, nil
}
