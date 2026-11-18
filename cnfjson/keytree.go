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

// keyTree design summary:
//
//  1. key is set from the Str.Data field of the respective key.
//  2. Paths are dissolved into individual keys in the keyTree.
//  3. Marshaling happens in two phases for every map:
//     a) Generate the keyTree, stopping the recursion on non-map values.
//     b) Marshal the keyTree, this time recursing into non-map values.
//  4. The 3a phase recurses from block maps into inline maps,
//     as these are part of the same map hierarchy.
//  5. The 3a phase does not recurse into lists, because although lists
//     may contain other maps, these are completely independent hierarchies
//     with no mixed paths.
type keyTree struct {
	key  string       // the segment text, the quote style dissolved
	sub  keyLevel     // the children: expansions and paths below
	item *cnfast.Item // the non-map definition tipping here, if one did
}

// A keyLevel is one level of the keyTree: its nodes in document
// order, indexed by key once the level is wide.
type keyLevel struct {
	nodes []*keyTree
	index map[string]*keyTree
}

// indexFrom is the width past which a level indexes its keys: a
// linear search is cheaper on the narrow maps most documents hold,
// and quadratic on wide ones.
const indexFrom = 16

func (l *keyLevel) find(key string) *keyTree {
	if l.index != nil {
		return l.index[key]
	}
	for _, n := range l.nodes {
		if n.key == key {
			return n
		}
	}
	return nil
}

func (l *keyLevel) add(node *keyTree) {
	l.nodes = append(l.nodes, node)
	switch {
	case l.index != nil:
		l.index[node.key] = node
	case len(l.nodes) > indexFrom:
		l.index = make(map[string]*keyTree, len(l.nodes))
		for _, n := range l.nodes {
			l.index[n.key] = n
		}
	}
}

// keyTreeFor collects one map's item list into its keyTree,
// in document order: the item that first names a key holds
// its node, later items expanding or descending through it.
func keyTreeFor(m *cnfast.Map) ([]*keyTree, error) {
	var level keyLevel
	for _, item := range m.Items {
		if err := placeKey(&level, item.Key, item); err != nil {
			return nil, err
		}
	}
	return level.nodes, nil
}

// placeKey dissolves one item into the node list: the key's
// segments walk one node each, taking or creating as they
// go, and the item lands at the tip - a map value expanding
// into the tip's children, the walk dissolving them the same
// way, a non-map value recorded as the tip's item.
func placeKey(level *keyLevel, key []*cnfast.Str, item *cnfast.Item) error {
	if len(key) == 0 {
		return nil
	}
	node := level.find(key[0].Data)
	if node == nil {
		node = &keyTree{key: key[0].Data}
		level.add(node)
	}
	if len(key) > 1 {
		// A path descending: only a non-map definition blocks
		// it, a map holding it open however it was spelled.
		if node.item != nil {
			return redefined(node, key[0])
		}
		return placeKey(&node.sub, key[1:], item)
	}
	if sub, ok := item.Value.(*cnfast.Map); ok {
		if node.item != nil {
			return redefined(node, key[0])
		}
		for _, it := range sub.Items {
			if err := placeKey(&node.sub, it.Key, it); err != nil {
				return err
			}
		}
		return nil
	}
	if node.item != nil || len(node.sub.nodes) > 0 {
		return redefined(node, key[0])
	}
	node.item = item
	return nil
}

// redefined reports one key's second definition, the marker
// positioned at the offending item's key segment: a tree the
// parser produced carries its source position, a hand-built
// one carries none, the message written without the prefix.
func redefined(node *keyTree, seg *cnfast.Str) error {
	msg := fmt.Sprintf("cannot redefine previous key `%s`", node.key)
	if seg.Pos.L == 0 {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("%d:%d: %s", seg.Pos.L, seg.Pos.C, msg)
}

// writeKeyTree writes one level of the keyTree as a JSON
// object: each node one pair, key first, then the children
// as a nested object, or the recorded item's value - a node
// holding neither is an empty map, a `a: {}` dissolved and
// merged like any other.
func writeKeyTree(b *strings.Builder, nodes []*keyTree) error {
	b.WriteString("{")
	for i, node := range nodes {
		if i > 0 {
			b.WriteString(",")
		}
		if err := writeQuoted(b, node.key); err != nil {
			return err
		}
		b.WriteString(":")
		switch {
		case len(node.sub.nodes) > 0:
			if err := writeKeyTree(b, node.sub.nodes); err != nil {
				return err
			}
		case node.item != nil:
			if err := writeValue(b, node.item.Value); err != nil {
				return err
			}
		default:
			b.WriteString("{}")
		}
	}
	b.WriteString("}")
	return nil
}
