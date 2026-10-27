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

// Clone deep-copies the whole item tree.
func Clone(item *Item) *Item {
	if item == nil {
		return nil
	}
	out := &Item{
		Key:     cloneKey(item.Key),
		PreCmt:  cloneCmt(item.PreCmt),
		HeadCmt: cloneCmt(item.HeadCmt),
		InCmt:   cloneCmt(item.InCmt),
		FootCmt: cloneCmt(item.FootCmt),
		PostCmt: cloneCmt(item.PostCmt),
		Indent:  item.Indent,
	}
	out.Value = cloneValue(item.Value)
	return out
}

func cloneKey(key []*Str) []*Str {
	if key == nil {
		return nil
	}
	out := make([]*Str, len(key))
	for i, k := range key {
		k2 := *k
		out[i] = &k2
	}
	return out
}

func cloneCmt(c Cmt) Cmt {
	if c.Text == nil {
		return Cmt{}
	}
	return Cmt{Text: append([]string{}, c.Text...), Pos: c.Pos}
}

func cloneValue(v Value) Value {
	switch v := v.(type) {
	case *Str:
		s := *v
		if v.Text != nil {
			s.Text = append([]string{}, v.Text...)
		}
		return &s
	case *Num:
		n := *v
		return &n
	case *Scalar:
		s := *v
		return &s
	case *Map:
		m := &Map{Pos: v.Pos, Block: v.Block}
		for _, it := range v.Items {
			m.Items = append(m.Items, Clone(it))
		}
		return m
	case *List:
		l := &List{Pos: v.Pos, Block: v.Block, Dashed: v.Dashed}
		for i := range v.Items {
			l.Items = append(l.Items, Clone(v.Items[i]))
		}
		return l
	}
	return nil
}
