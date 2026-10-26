package cnfmap_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/canonical/now/internal/cnf/cnfmap"
	"github.com/canonical/now/internal/cnf/cnftest"
)

// TestCases runs the corpus against cnfmap, the direct
// value decoder: the to-json direction compares by value -
// the cnf block parsed, the json block decoded, the two
// compared. The from-json direction takes the other road to
// the same model: the json block decoded through
// encoding/json, the Go value marshaled back to cnf, the
// cnf block parsed, the two compared by value. A
// direction the value model cannot see skips: the one
// from-json error case rides on duplicate JSON keys, which
// encoding/json resolves last-wins instead of failing.
func TestCases(t *testing.T) {
	topics, err := cnftest.ReadAll("../spec/cases")
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range topics {
		for _, c := range topic.Cases {
			t.Run(topic.Name+"/"+c.Name, func(t *testing.T) {
				runCase(t, c)
			})
		}
	}
}

// runCase runs one case: each declared direction through its
// runner, both when the case declares the pair.
func runCase(t *testing.T, c *cnftest.Case) {
	t.Helper()
	ran := false
	if c.ToJSON.Kind != "" {
		runToJSON(t, c)
		ran = true
	}
	if c.FromJSON.Kind != "" {
		runFromJSON(t, c)
		ran = true
	}
	if !ran {
		t.Skip("cnfmap handles to-json and from-json only")
	}
}

// runToJSON executes one case's to-json direction: every
// cnf block parsed, compared by value against the first
// json block.
func runToJSON(t *testing.T, c *cnftest.Case) {
	t.Helper()
	if c.ToJSON.IsError() {
		for _, in := range c.CNF {
			_, err := cnfmap.Unmarshal([]byte(in))
			if err == nil {
				t.Errorf("to-json: no error, want %q", c.Error.Text)
				continue
			}
			ok, merr := cnftest.MatchError(c, in, err)
			if merr != nil {
				t.Errorf("to-json: broken case: %s", merr)
				continue
			}
			if !ok {
				t.Errorf("to-json error:\n  got  %q\n  want %s", err.Error(), c.Error.Text)
			}
		}
		return
	}
	if len(c.JSON) == 0 {
		t.Errorf("to-json without a json block")
		return
	}
	for _, in := range c.CNF {
		// The expected text may reference the case's vars,
		// located in this input block.
		want, xerr := cnftest.ExpandVars(c, in, c.JSON[0])
		if xerr != nil {
			t.Errorf("to-json: broken case: %s", xerr)
			return
		}
		var wantVal any
		if err := json.Unmarshal([]byte(want), &wantVal); err != nil {
			t.Errorf("cannot decode want %q: %s", want, err)
			return
		}
		got, err := cnfmap.Unmarshal([]byte(in))
		if err != nil {
			t.Errorf("to-json: %s", err)
			continue
		}
		if !reflect.DeepEqual(got, wantVal) {
			t.Errorf("to-json value:\n  got  %#v\n  want %#v", got, wantVal)
		}
	}
}

// runFromJSON executes one case's from-json direction:
// each json block decodes through encoding/json into a Go
// value, the value marshals back to cnf, and the cnf
// compares by value against the first cnf block - both
// sides parse through cnfmap, so the spellings and layouts
// the marshaler picks differently from the case file
// disappear. An error directive skips: the model cannot
// produce one, since encoding/json resolves a duplicate
// key instead of failing.
func runFromJSON(t *testing.T, c *cnftest.Case) {
	t.Helper()
	if c.FromJSON.IsError() {
		t.Skip("from-json error cases need duplicate-key detection, which encoding/json does not offer")
	}
	if len(c.CNF) == 0 {
		t.Errorf("from-json without a cnf block")
		return
	}
	for _, in := range c.JSON {
		// The input may reference the case's vars, the groups
		// read from this input block.
		in, xerr := cnftest.ExpandVars(c, in, in)
		if xerr != nil {
			t.Errorf("from-json: broken case: %s", xerr)
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(in), &v); err != nil {
			t.Errorf("from-json: cannot decode json block: %s", err)
			continue
		}
		gotCNF, err := cnfmap.Marshal(v)
		if err != nil {
			t.Errorf("from-json: %s", err)
			continue
		}
		got, err := cnfmap.Unmarshal(gotCNF)
		if err != nil {
			t.Errorf("from-json: marshal output does not re-parse: %s\n cnf:\n%s", err, gotCNF)
			continue
		}
		// The expected text may reference the case's vars,
		// located in this input block.
		want, xerr := cnftest.ExpandVars(c, in, c.CNF[0])
		if xerr != nil {
			t.Errorf("from-json: broken case: %s", xerr)
			continue
		}
		wantVal, err := cnfmap.Unmarshal([]byte(want))
		if err != nil {
			t.Errorf("from-json: broken case: cnf block does not parse: %s", err)
			continue
		}
		if !reflect.DeepEqual(got, wantVal) {
			t.Errorf("from-json value:\n  got  %#v\n  want %#v\n cnf:\n%s", got, wantVal, gotCNF)
		}
	}
}
