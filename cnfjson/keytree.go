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
	sub  []*keyTree   // the children: expansions and paths below
	item *cnfast.Item // the non-map definition tipping here, if one did
}

// keyTreeFor collects one map's item list into its keyTree,
// in document order: the item that first names a key holds
// its node, later items expanding or descending through it.
func keyTreeFor(m *cnfast.Map) ([]*keyTree, error) {
	var nodes []*keyTree
	for _, item := range m.Items {
		var err error
		nodes, err = placeKey(nodes, item.Key, item)
		if err != nil {
			return nil, err
		}
	}
	return nodes, nil
}

// placeKey dissolves one item into the node list: the key's
// segments walk one node each, taking or creating as they
// go, and the item lands at the tip - a map value expanding
// into the tip's children, the walk dissolving them the same
// way, a non-map value recorded as the tip's item.
func placeKey(nodes []*keyTree, key []*cnfast.Str, item *cnfast.Item) ([]*keyTree, error) {
	if len(key) == 0 {
		return nodes, nil
	}
	node, pos := findKey(nodes, key[0].Data)
	if pos < 0 {
		node = &keyTree{key: key[0].Data}
		nodes = append(nodes, node)
	}
	if len(key) > 1 {
		// A path descending: only a non-map definition blocks
		// it, a map holding it open however it was spelled.
		if node.item != nil {
			return nodes, redefined(node, key[0])
		}
		sub, err := placeKey(node.sub, key[1:], item)
		if err != nil {
			return nodes, err
		}
		node.sub = sub
		return nodes, nil
	}
	if sub, ok := item.Value.(*cnfast.Map); ok {
		if node.item != nil {
			return nodes, redefined(node, key[0])
		}
		for _, it := range sub.Items {
			var err error
			node.sub, err = placeKey(node.sub, it.Key, it)
			if err != nil {
				return nodes, err
			}
		}
		return nodes, nil
	}
	if node.item != nil || node.sub != nil {
		return nodes, redefined(node, key[0])
	}
	node.item = item
	return nodes, nil
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

// findKey locates one segment's node in a level of the tree.
func findKey(nodes []*keyTree, key string) (*keyTree, int) {
	for i, n := range nodes {
		if n.key == key {
			return n, i
		}
	}
	return nil, -1
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
		case len(node.sub) > 0:
			if err := writeKeyTree(b, node.sub); err != nil {
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
