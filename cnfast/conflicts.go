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

// The expansion tracking: no map key is expanded twice, and no key is
// defined both as a map and as a non-map - paths traversing their prefix
// are the one exemption. The check walks the built AST with a trie, one
// level per map scope; a level is shared by the map it expands, so
// later converging paths read the history the descent wrote.

// A trackedKey is the history of one key at one map scope.
type trackedKey struct {
	expanded bool       // a map was defined here
	conflict bool       // a non-map - scalar or list - was defined here
	values   trackedMap // the next level down
}

// A trackedMap is one scope of tracked keys.
type trackedMap map[string]trackedKey

// checkConflicts validates the expansion rule over one parsed tree.
// The double non-map definition stays legal - the merge layer owns
// that conflict.
func checkConflicts(root *Item) error {
	switch v := root.Value.(type) {
	case *Map:
		return checkMapConflicts(v, trackedMap{})
	case *List:
		return checkListConflicts(v)
	}
	return nil
}

// checkListConflicts walks the map scopes inside list elements. Paths
// cannot cross into a list, so every element map starts a fresh scope.
func checkListConflicts(l *List) error {
	for _, elem := range l.Items {
		switch v := elem.Value.(type) {
		case *Map:
			if err := checkMapConflicts(v, trackedMap{}); err != nil {
				return err
			}
		case *List:
			if err := checkListConflicts(v); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkMapConflicts checks one map's item list against one scope of the
// trie, and descends into every item's value. Document order decides: the
// first definition stands, the one contradicting it takes the error.
func checkMapConflicts(m *Map, lastValues trackedMap) error {
	for _, item := range m.Items {
		if sub, ok := item.Value.(*Map); ok {
			nextValues, err := trackKey(lastValues, item.Key, false)
			if err != nil {
				return err
			}
			// The expansion's own map shares the level the tip recorded:
			// items inside it and later paths converging there see one
			// history.
			if err := checkMapConflicts(sub, nextValues); err != nil {
				return err
			}
			continue
		}
		// A non-map tip is a definition the tracking must see;
		// a list walks its elements after.
		if _, err := trackKey(lastValues, item.Key, true); err != nil {
			return err
		}
		if l, ok := item.Value.(*List); ok {
			if err := checkListConflicts(l); err != nil {
				return err
			}
		}
	}
	return nil
}

// trackKey walks one item's key path through the trie, conflict telling
// whether the item defines a non-map. Non-tip segments are never errors;
// at the tip, a map over a prior expansion, or any second definition,
// is a redefinition.
func trackKey(lastValues trackedMap, key []*Str, conflict bool) (trackedMap, error) {
	nextKey, seen := lastValues[key[0].Data]
	if !seen {
		nextKey = trackedKey{values: trackedMap{}}
		lastValues[key[0].Data] = nextKey
	}
	if len(key) > 1 {
		return trackKey(nextKey.values, key[1:], conflict)
	}
	if !conflict {
		if nextKey.expanded {
			return nil, errAt(key[0].Pos, "cannot expand map key "+keyPath(key)+" more than once")
		}
		if nextKey.conflict {
			return nil, errAt(key[0].Pos, "cannot redefine previous key "+keyPath(key))
		}
		nextKey.expanded = true
		lastValues[key[0].Data] = nextKey
		return nextKey.values, nil
	}
	if seen {
		return nil, errAt(key[0].Pos, "cannot redefine previous key "+keyPath(key))
	}
	nextKey.conflict = true
	lastValues[key[0].Data] = nextKey
	return nil, nil
}
