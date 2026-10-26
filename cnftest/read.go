package cnftest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A Topic is one cases/<name>.md file: a # Topic header, a prose
// summary, and one or more Cases.
type Topic struct {
	Name  string
	Cases []*Case
}

// A Case is one ## Case: <name> section: a prose summary, a
// fenced directive block, and the ### sections with their
// fenced blocks. The name respects the bare word convention
// and is unique within its topic. The directives a harness does
// not know are kept but left unread; each harness reads the
// ones it runs.
type Case struct {
	Name     string
	Summary  string
	ToJSON   Directive // "" when the direction is absent
	FromJSON Directive
	ToAST    Directive
	FromAST  Directive
	Error    Data            // the expected message of an error direction
	Vars     map[string]Data // the vars: each name its expectation
	Reformat Reformat        // the reformat: block's settings, the corpus vocabulary
	ParseAST bool            // the ast blocks are spelled as cnf, converted at run time
	CNF      []string
	JSON     []string
	AST      []string
}

// A Reformat is the reformat-printing settings a case declares in
// its reformat: directive block: the corpus's settings vocabulary,
// one field per named setting. The names are the block's keys,
// kebab-case; a harness maps the settings onto its own render
// options - the vocabulary is the conformance package's, the
// settings are the implementation's. Zero means default, per
// field, under the implementation's resolution rules.
type Reformat struct {
	UnfoldListFromItems     int // unfold-list-from-items
	UnfoldMapFromItems      int // unfold-map-from-items
	FoldListUptoItems       int // fold-list-upto-items
	FoldMapUptoItems        int // fold-map-upto-items
	FoldPathUptoDepth       int // fold-path-upto-depth
	FoldPathFromDepth       int // fold-path-from-depth
	UnfoldPathFromDepth     int // unfold-path-from-depth
	UnfoldPathUptoDepth     int // unfold-path-upto-depth
	SoftWrapStringFromWidth int // soft-wrap-string-from-width
	HardWrapStringFromWidth int // hard-wrap-string-from-width

	Reindent       string // reindent
	IgnoreComments bool   // ignore-comments
}

// set sets one named reformat setting. An unknown name is a
// malformed case: the vocabulary is fixed here.
// IsZero reports whether a case's reformat settings are all
// unset: a case with no reformat: block, or an empty one. A
// harness that does not reformat - rendering byte-faithfully -
// skips the case entirely, the reformat harness running it.
func (p Reformat) IsZero() bool {
	return p == Reformat{}
}

func (p *Reformat) set(name string, val int) error {
	switch name {
	case "unfold-list-from-items":
		p.UnfoldListFromItems = val
	case "unfold-map-from-items":
		p.UnfoldMapFromItems = val
	case "fold-list-upto-items":
		p.FoldListUptoItems = val
	case "fold-map-upto-items":
		p.FoldMapUptoItems = val
	case "fold-path-upto-depth":
		p.FoldPathUptoDepth = val
	case "fold-path-from-depth":
		p.FoldPathFromDepth = val
	case "unfold-path-from-depth":
		p.UnfoldPathFromDepth = val
	case "unfold-path-upto-depth":
		p.UnfoldPathUptoDepth = val
	case "soft-wrap-string-from-width":
		p.SoftWrapStringFromWidth = val
	case "hard-wrap-string-from-width":
		p.HardWrapStringFromWidth = val
	case "reindent":
		return fmt.Errorf("reformat setting %q is not an int", name)
	default:
		return fmt.Errorf("unknown reformat setting %q", name)
	}
	return nil
}

// A Directive is one direction's expected outcome: advised-text,
// exact-value, advised-value, or error. The text
// operators compare identically - the names document how strong
// the recommendation is: advised-text for outputs that are
// fully determined, advised-text for outputs the writer's
// heuristics chose among valid spellings. The error kind carries
// its expectation in the case's error field: a Data value - a
// literal value, or a {regexp: "..."} map value. Any expected
// text may reference the case's vars, resolved by ExpandVars.
type Directive struct {
	Kind string // "", "advised-text", "exact-value", "advised-value", "error"
}

// IsError reports whether the directive expects an error.
func (d Directive) IsError() bool {
	return d.Kind == "error"
}

// ReadAll reads every *.md file under dir (skipping README) into
// topics. It fails when a file is malformed.
func ReadAll(dir string) ([]Topic, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no case files found in %s", dir)
	}
	var topics []Topic
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".md")
		if name == "README" {
			continue // documentation, not a case
		}
		t, err := readTopic(file)
		if err != nil {
			return nil, fmt.Errorf("%s: %s", name, err)
		}
		if len(t.Cases) == 0 {
			return nil, fmt.Errorf("%s: no cases found", name)
		}
		for _, c := range t.Cases {
			if err := validateBlocks(c); err != nil {
				return nil, fmt.Errorf("%s/%s: %s", name, c.Name, err)
			}
		}
		topics = append(topics, *t)
	}
	return topics, nil
}

// validateBlocks checks the machine-read blocks of a case:
// each ast and json block must be exactly one JSON value, with
// nothing but blanks from it, and each block must have a
// directive that reads it. A decoder alone tolerates trailing
// content - it stops at the first complete value - so the
// remainder is checked explicitly.
func validateBlocks(c *Case) error {
	if !c.ParseAST {
		// The ast blocks are written as cnf: not JSON to validate,
		// converted at run time instead.
		for _, block := range c.AST {
			if err := oneJSONValue("ast", block); err != nil {
				return err
			}
		}
	}
	for _, block := range c.JSON {
		if err := oneJSONValue("json", block); err != nil {
			return err
		}
	}
	// A block no directive reads is a case authoring bug: its
	// content is never verified, so a stale or wrong block
	// would pass unnoticed. An error directive reads only its
	// own input, not the block it would compare against.
	// The cnf blocks feed to-json and to-ast as inputs and
	// from-json and from-ast as expectations; the json blocks
	// the reverse; the ast blocks to-ast's comparison and
	// from-ast's input.
	readsCNF := c.ToJSON.Kind != "" || c.ToAST.Kind != "" ||
		c.FromJSON.Kind != "" && !c.FromJSON.IsError() ||
		c.FromAST.Kind != "" && !c.FromAST.IsError()
	readsJSON := c.FromJSON.Kind != "" ||
		c.ToJSON.Kind != "" && !c.ToJSON.IsError()
	readsAST := c.FromAST.Kind != "" ||
		c.ToAST.Kind != "" && !c.ToAST.IsError()
	if len(c.CNF) > 0 && !readsCNF {
		return fmt.Errorf("cnf block without a directive that reads it")
	}
	if len(c.JSON) > 0 && !readsJSON {
		return fmt.Errorf("json block without a directive that reads it")
	}
	if len(c.AST) > 0 && !readsAST {
		return fmt.Errorf("ast block without a directive that reads it")
	}
	return nil
}

// oneJSONValue reports an error when text does not hold exactly
// one JSON value: a decode failure, or trailing non-blank
// content from the value. A decoder alone tolerates trailing
// content - it stops at the first complete value - so the
// offset from the decode is checked against the text's length.
// The value decodes into a RawMessage: validation only, no
// materialization.
func oneJSONValue(kind, text string) error {
	dec := json.NewDecoder(strings.NewReader(text))
	var v json.RawMessage
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("%s block is not JSON: %s", kind, err)
	}
	rest := strings.TrimSpace(text[dec.InputOffset():])
	if rest != "" {
		r := rest
		if len(r) > 40 {
			r = r[:40]
		}
		return fmt.Errorf("%s block has trailing content after the value: %q", kind, r)
	}
	return nil
}

// readTopic parses one case file: the # Topic header, the prose
// summary, and the ## Case sections, each with its fenced
// directive block and its ### sections.
func readTopic(file string) (*Topic, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	t := &Topic{Name: strings.TrimSuffix(filepath.Base(file), ".md")}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "# ") {
		return nil, fmt.Errorf("cannot find opening # header")
	}
	var cs *Case
	var section string
	var block []string
	inFence := false
	varsMode := false
	prettyMode := false
	prettySeen := map[string]bool{}
	flush := func() {
		if section == "" {
			return
		}
		// An empty cnf block is meaningful: the empty file.
		if len(block) == 0 && section != "cnf" {
			return
		}
		text := strings.Join(block, "\n") + "\n"
		if section == "cnf" && len(block) == 0 {
			text = ""
		}
		switch section {
		case "cnf":
			cs.CNF = append(cs.CNF, text)
		case "json":
			cs.JSON = append(cs.JSON, text)
		case "ast":
			cs.AST = append(cs.AST, text)
		}
		block = nil
	}
	for _, ln := range lines[1:] {
		switch {
		case strings.HasPrefix(ln, "## Case"):
			name, err := caseName(ln)
			if err != nil {
				return nil, err
			}
			for _, prev := range t.Cases {
				if prev.Name == name {
					return nil, fmt.Errorf("duplicate case name %q", name)
				}
			}
			// No flush here: every content fence flushes at its
			// close, and a flush with no new block would append
			// an empty cnf block spuriously.
			cs = &Case{Name: name}
			prettySeen = map[string]bool{}
			t.Cases = append(t.Cases, cs)
			section = ""
			inFence = false
		case cs == nil:
			continue // prose before the first case
		case inFence:
			if strings.HasPrefix(ln, "```") {
				if section != "" {
					flush()
				}
				inFence = false
				varsMode = false
				prettyMode = false
				continue
			}
			if section != "" {
				block = append(block, ln)
				continue
			}
			if strings.TrimSpace(ln) == "" {
				continue
			}
			if varsMode && (ln[0] == ' ' || ln[0] == '\t') {
				name, data, err := parseHeaderValue(ln)
				if err != nil {
					return nil, fmt.Errorf("%s: %s", ln, err.Error())
				}
				if !isVarName(name) {
					return nil, fmt.Errorf("var name %q is not [a-z]+", name)
				}
				if _, dup := cs.Vars[name]; dup {
					return nil, fmt.Errorf("duplicate var %q", name)
				}
				if cs.Vars == nil {
					cs.Vars = map[string]Data{}
				}
				cs.Vars[name] = data
				continue
			}
			if prettyMode && (ln[0] == ' ' || ln[0] == '\t') {
				colon := strings.Index(ln, ":")
				if colon < 0 {
					return nil, fmt.Errorf("%s: cannot find key-value separator", ln)
				}
				name := strings.TrimSpace(ln[:colon])
				if _, dup := prettySeen[name]; dup {
					return nil, fmt.Errorf("duplicate reformat setting %q", name)
				}
				prettySeen[name] = true
				if name == "reindent" {
					// The one string-valued setting, parsed
					// the same way the vars values are: a cnf
					// double-quoted string, the escapes it
					// supports.
					_, val, perr := parseHeaderPair(ln)
					if perr != nil {
						return nil, fmt.Errorf("%s: %s", ln, perr.Error())
					}
					cs.Reformat.Reindent = val
					continue
				}
				if name == "ignore-comments" {
					// The one bool setting, the same side path
					// reindent takes around set - the vocabulary
					// there is ints only.
					b, berr := strconv.ParseBool(strings.TrimSpace(ln[colon+1:]))
					if berr != nil {
						return nil, fmt.Errorf("%s: reformat setting %q: %s", ln, name, berr)
					}
					cs.Reformat.IgnoreComments = b
					continue
				}
				n, nerr := strconv.Atoi(strings.TrimSpace(ln[colon+1:]))
				if nerr != nil {
					return nil, fmt.Errorf("%s: reformat setting %q: %s", ln, name, nerr)
				}
				if serr := cs.Reformat.set(name, n); serr != nil {
					return nil, fmt.Errorf("%s: %s", ln, serr)
				}
				continue
			}
			if strings.TrimSpace(ln) == "vars:" {
				varsMode = true
				prettyMode = false
				continue
			}
			if strings.TrimSpace(ln) == "reformat:" {
				prettyMode = true
				varsMode = false
				continue
			}
			if strings.HasPrefix(ln, "parse-ast:") {
				val := strings.TrimSpace(strings.TrimPrefix(ln, "parse-ast:"))
				if val != "true" {
					return nil, fmt.Errorf("%s: parse-ast expects true, got %q", ln, val)
				}
				cs.ParseAST = true
				continue
			}
			key, data, err := parseHeaderValue(ln)
			if err != nil {
				return nil, fmt.Errorf("%s: %s", ln, err.Error())
			}
			// Only error: and the vars take the regexp map; the
			// four directions hold one fixed vocabulary word.
			if data.Regexp && key != "error" {
				return nil, fmt.Errorf("%s: a regexp map is not allowed here", ln)
			}
			switch key {
			case "to-json":
				cs.ToJSON = parseDirective(data.Text)
			case "from-json":
				cs.FromJSON = parseDirective(data.Text)
			case "to-ast":
				cs.ToAST = parseDirective(data.Text)
			case "from-ast":
				cs.FromAST = parseDirective(data.Text)
			case "error":
				cs.Error = data
			default:
				return nil, fmt.Errorf("unknown header key %q", key)
			}
		case strings.HasPrefix(ln, "### "):
			// No flush here either: the closing fence of the
			// previous section already flushed it.
			section = strings.TrimSpace(strings.TrimPrefix(ln, "### "))
		case strings.HasPrefix(ln, "```"):
			inFence = true
		}
	}
	// The final flush catches a file that ends inside a fence;
	// a closed fence already flushed, and re-flushing would
	// append a spurious empty cnf block.
	if inFence {
		flush()
	}
	return t, nil
}

// caseName reads the name from a "## Case: <name>" header,
// validating the bare word convention.
func caseName(ln string) (string, error) {
	name := strings.TrimSpace(strings.TrimPrefix(ln, "## Case:"))
	if name == "" {
		return "", fmt.Errorf("case header without a name")
	}
	for _, r := range name {
		if !isBareRune(r) {
			return "", fmt.Errorf("case name %q is not a bare word", name)
		}
	}
	return name, nil
}

// isVarName reports whether name is [a-z]+.
func isVarName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func isBareRune(r rune) bool {
	return r == '-' || r == '_' ||
		('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')
}

// parseHeaderValue splits one `key: value` line of the header,
// the value a Data: a cnf double-quoted string - the
// literal - or a single-key map with the key regexp - the
// pattern. Only the escapes the directives need are handled,
// keeping this reader independent of the implementations
// under test.
func parseHeaderValue(ln string) (key string, data Data, err error) {
	colon := strings.Index(ln, ":")
	if colon < 0 {
		return "", data, fmt.Errorf("cannot find key-value separator")
	}
	key = strings.TrimSpace(ln[:colon])
	rest := strings.TrimSpace(ln[colon+1:])
	if strings.HasPrefix(rest, "{") {
		// The map form: {regexp: "..."} exactly - one key,
		// the name regexp, nothing else.
		if !strings.HasPrefix(rest, "{regexp:") {
			return "", data, fmt.Errorf("cannot parse value %q: the map key must be regexp", rest)
		}
		val := strings.TrimSpace(strings.TrimPrefix(rest, "{regexp:"))
		if !strings.HasSuffix(val, "}") {
			return "", data, fmt.Errorf("cannot parse value %q: the map does not close", rest)
		}
		val = strings.TrimSpace(strings.TrimSuffix(val, "}"))
		if val == "" {
			return "", data, fmt.Errorf("cannot parse value %q: the map value is empty", rest)
		}
		s, uerr := unquote(val)
		if uerr != nil {
			return "", data, uerr
		}
		if _, cerr := regexp.Compile(s); cerr != nil {
			return "", data, fmt.Errorf("bad regexp %q: %s", s, cerr)
		}
		return key, Data{Text: s, Regexp: true}, nil
	}
	s, uerr := unquote(rest)
	if uerr != nil {
		return "", data, uerr
	}
	return key, Data{Text: s}, nil
}

// parseHeaderPair splits one `key: "value"` line of the header:
// the plain-string form, without the regexp map.
func parseHeaderPair(ln string) (key, val string, err error) {
	k, data, perr := parseHeaderValue(ln)
	if perr != nil {
		return "", "", perr
	}
	if data.Regexp {
		return "", "", fmt.Errorf("cannot parse value %q: a regexp map is not allowed here", data.Text)
	}
	return k, data.Text, nil
}

// unquote reads one cnf double-quoted string, the value's
// body with its escapes decoded.
func unquote(rest string) (string, error) {
	if len(rest) < 2 || rest[0] != '"' || rest[len(rest)-1] != '"' {
		return "", fmt.Errorf("cannot parse value %q", rest)
	}
	var b strings.Builder
	for i := 1; i < len(rest)-1; i++ {
		if rest[i] == '\\' && i+1 < len(rest)-1 {
			i++
			switch rest[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'u':
				// The \u escape takes exactly four hex
				// digits, all inside the quotes - the BMP
				// range. The uppercase \U takes eight,
				// reaching the astral planes.
				if i+4 >= len(rest)-1 {
					return "", fmt.Errorf("cannot parse escape \\u: wants four hex digits")
				}
				n, herr := strconv.ParseUint(rest[i+1:i+5], 16, 32)
				if herr != nil || !isHexRun(rest[i+1:i+5]) {
					return "", fmt.Errorf("cannot parse escape \\u%s: wants four hex digits", rest[i+1:i+5])
				}
				b.WriteRune(rune(n))
				i += 4
			case 'U':
				if i+8 >= len(rest)-1 || !isHexRun(rest[i+1:i+9]) {
					return "", fmt.Errorf("cannot parse escape \\U: wants eight hex digits")
				}
				n, herr := strconv.ParseUint(rest[i+1:i+9], 16, 32)
				if herr != nil || n > 0x10FFFF {
					return "", fmt.Errorf("cannot parse escape \\U%s: out of range", rest[i+1:i+9])
				}
				b.WriteRune(rune(n))
				i += 8
			case '"', '\\':
				b.WriteByte(rest[i])
			default:
				return "", fmt.Errorf("cannot parse escape \\%c", rest[i])
			}
			continue
		}
		b.WriteByte(rest[i])
	}
	return b.String(), nil
}

// isHexRun reports whether s is a non-empty run of hex digits.
func isHexRun(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// parseDirective reads one directive value.
func parseDirective(val string) Directive {
	switch val {
	case "advised-text", "exact-value", "advised-value", "error":
		return Directive{Kind: val}
	}
	return Directive{Kind: val} // unknown: reported at run time
}
