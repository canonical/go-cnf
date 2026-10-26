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
	"fmt"
)

// UnmarshalJSON reads the JSON form of an AST - the review form
// that MarshalJSON produces - back into AST nodes. The document
// is always an item, so the top-level form is the keyless root
// item, dispatched on its "value" field - anything else at the
// top violates the invariant and is rejected. The defining
// field's presence says what each nested value is: str, num,
// scalar, list, map. Key segments unmarshal directly (their
// fields match Str's tags); only the any-typed values need the
// dispatch.
func UnmarshalJSON(data []byte) (any, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw["value"] == nil {
		return nil, fmt.Errorf("cannot decode document: no root item")
	}
	// The root item is keyless: the value with its comment
	// runs, block indent, and content indent.
	var wrap struct {
		Value   json.RawMessage `json:"value"`
		PreCmt  Cmt             `json:"precmt"`
		HeadCmt Cmt             `json:"headcmt"`
		InCmt   Cmt             `json:"incmt"`
		FootCmt Cmt             `json:"footcmt"`
		PostCmt Cmt             `json:"postcmt"`
		Indent  string          `json:"indent"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(wrap.Value, &v); err != nil {
		return nil, err
	}
	val, err := decodeValue(v)
	if err != nil {
		return nil, err
	}
	return &Item{
		Value:   val,
		PreCmt:  wrap.PreCmt,
		HeadCmt: wrap.HeadCmt,
		InCmt:   wrap.InCmt,
		FootCmt: wrap.FootCmt,
		PostCmt: wrap.PostCmt,
		Indent:  wrap.Indent,
	}, nil
}

// decodeValue builds one value node from its raw form, which
// plain unmarshaling left as a generic map.
func decodeValue(v any) (Value, error) {
	raw, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("cannot decode value %v", v)
	}
	switch {
	case raw["str"] != nil:
		s := &Str{Data: raw["str"].(string)}
		if q, ok := raw["quote"].(float64); ok {
			s.Quote = QuoteKind(int(q))
		}
		if blk, ok := raw["block"].(bool); ok {
			s.Block = blk
		}
		if p, ok := raw["pos"].(map[string]any); ok {
			decodePos(p, &s.Pos)
		}
		return s, nil
	case raw["num"] != nil:
		n := &Num{Text: raw["num"].(string)}
		if p, ok := raw["pos"].(map[string]any); ok {
			decodePos(p, &n.Pos)
		}
		return n, nil
	case raw["scalar"] != nil:
		s := &Scalar{Text: raw["scalar"].(string)}
		if p, ok := raw["pos"].(map[string]any); ok {
			decodePos(p, &s.Pos)
		}
		return s, nil
	case raw["list"] != nil:
		elems, err := decodeList(raw["list"])
		if err != nil {
			return nil, err
		}
		l := &List{Items: elems}
		if blk, ok := raw["block"].(bool); ok {
			l.Block = blk
		}
		if cmp, ok := raw["dashed"].(bool); ok {
			l.Dashed = cmp
		}
		if p, ok := raw["pos"].(map[string]any); ok {
			decodePos(p, &l.Pos)
		}
		return l, nil
	case raw["map"] != nil:
		pairs, err := decodeItems(raw["map"])
		if err != nil {
			return nil, err
		}
		if pairs == nil {
			pairs = []*Item{} // an empty map: [] or null
		}
		m := &Map{Items: pairs}
		if blk, ok := raw["block"].(bool); ok {
			m.Block = blk
		}
		if p, ok := raw["pos"].(map[string]any); ok {
			decodePos(p, &m.Pos)
		}
		return m, nil
	}
	return nil, fmt.Errorf("cannot decode value: no defining field")
}

// decodeItems builds the pairs of a map value. The items arrive
// either as generic maps (a nested map decoded by the plain
// unmarshal) or as raw messages (the top level, where the
// Value interface forces the raw stage).
// decodeItem builds one item from its raw form: the key
// present when the element carries a path, the value through
// the shared decodeValue.
func decodeItem(m map[string]any) (Item, error) {
	item := Item{}
	if ks, ok := m["key"].([]any); ok {
		for _, k := range ks {
			km, ok := k.(map[string]any)
			if !ok {
				return item, fmt.Errorf("cannot decode key %v", k)
			}
			s := &Str{}
			if str, ok := km["str"].(string); ok {
				s.Data = str
			}
			if q, ok := km["quote"].(float64); ok {
				s.Quote = QuoteKind(int(q))
			}
			if p, ok := km["pos"].(map[string]any); ok {
				decodePos(p, &s.Pos)
			}
			item.Key = append(item.Key, s)
		}
	}
	val, err := decodeValue(m["value"])
	if err != nil {
		return item, err
	}
	item.Value = val
	decodeCmt(m["precmt"], &item.PreCmt)
	decodeCmt(m["headcmt"], &item.HeadCmt)
	decodeCmt(m["incmt"], &item.InCmt)
	decodeCmt(m["footcmt"], &item.FootCmt)
	decodeCmt(m["postcmt"], &item.PostCmt)
	if ind, ok := m["indent"].(string); ok {
		item.Indent = ind
	}
	return item, nil
}

func decodeItems(v any) ([]*Item, error) {
	var items []map[string]any
	switch raw := v.(type) {
	case []any:
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("cannot decode item %v", item)
			}
			items = append(items, m)
		}
	case []json.RawMessage:
		for _, item := range raw {
			var m map[string]any
			if err := json.Unmarshal(item, &m); err != nil {
				return nil, fmt.Errorf("cannot decode item %v", item)
			}
			items = append(items, m)
		}
	default:
		return nil, fmt.Errorf("cannot decode pairs %v", v)
	}
	var pairs []*Item
	for _, m := range items {
		item, err := decodeItem(m)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, &item)
	}
	return pairs, nil
}

// decodeList builds the pairs of a list value, which plain
// unmarshaling left as generic maps: the key present when the
// element carries a path, absent for a bare inline value.
func decodeList(v any) ([]*Item, error) {
	raw, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("cannot decode list %v", v)
	}
	elems := []*Item{}
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("cannot decode list element %v", item)
		}
		item, err := decodeItem(m)
		if err != nil {
			return nil, err
		}
		elems = append(elems, &item)
	}
	return elems, nil
}

// decodeCmt reads one comment run, absent when nil.
func decodeCmt(v any, c *Cmt) {
	if m, ok := v.(map[string]any); ok {
		if ls, ok := m["cmt"].([]any); ok {
			for _, l := range ls {
				if s, ok := l.(string); ok {
					c.Text = append(c.Text, s)
				}
			}
		}
		if p, ok := m["pos"].(map[string]any); ok {
			decodePos(p, &c.Pos)
		}
	}
}

// decodePos reads a position object.
func decodePos(p map[string]any, pos *Pos) {
	if l, ok := p["l"].(float64); ok {
		pos.L = int(l)
	}
	if c, ok := p["c"].(float64); ok {
		pos.C = int(c)
	}
}
