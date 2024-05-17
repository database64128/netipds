package netipmap

import (
	"fmt"
)

// tree is a binary radix tree with path compression.
type tree[T any] struct {
	label label
	value T
	left  *tree[T]
	right *tree[T]

	// Not every node has a value. A node may be just a shared prefix.
	hasValue bool
}

// newTree returns a new tree with the provided label.
func newTree[T any](l label) *tree[T] {
	return &tree[T]{label: l}
}

// clearValue removes the value from t.
func (t *tree[T]) clearValue() {
	var zeroVal T
	t.value = zeroVal
	t.hasValue = false
}

// withValue sets t's value to v and returns t.
func (t *tree[T]) withValue(v T) *tree[T] {
	t.value = v
	t.hasValue = true
	return t
}

// withValueFrom sets t's value to m's value and returns t.
func (t *tree[T]) withValueFrom(b *tree[T]) *tree[T] {
	if b == nil {
		return t
	}
	if b.hasValue {
		return t.withValue(b.value)
	}
	return t
}

// moveValueFrom moves m's value to t (removing it from m) and returns t.
func (t *tree[T]) moveValueFrom(b *tree[T]) *tree[T] {
	if b == nil {
		return t
	}
	if b.hasValue {
		t.value, t.hasValue = b.value, true
		b.clearValue()
	}
	return t
}

// withChildren sets t's children to the provided left and right trees and
// returns t.
func (t *tree[T]) withChildren(left *tree[T], right *tree[T]) *tree[T] {
	t.left = left
	t.right = right
	return t
}

// copyChildrenFrom sets t's children to copies of m's children and returns t.
func (t *tree[T]) copyChildrenFrom(b *tree[T]) *tree[T] {
	if b == nil {
		return t
	}
	return t.withChildren(b.left.copy(), b.right.copy())
}

// moveChildrenFrom moves m's children to t (removing them from m) and returns t.
func (t *tree[T]) moveChildrenFrom(b *tree[T]) *tree[T] {
	if b == nil {
		return t
	}
	t, _ = t.withChildren(b.left, b.right), b.withChildren(nil, nil)
	return t
}

// copy returns a copy of t, creating copies of all descendants of t in the
// process.
func (t *tree[T]) copy() *tree[T] {
	if t == nil {
		return nil
	}
	return newTree[T](t.label).copyChildrenFrom(t).withValueFrom(t)
}

// isZero returns true if this node's label is the zero label.
// TODO: change name to isRoot?
func (t *tree[T]) isZero() bool {
	return t.label.isZero()
}

// prettyPrint prints the tree in a human-readable format.
func (t *tree[T]) prettyPrint(indent string, prefix string) {
	if t == nil {
		return
	}

	fmt.Printf("%s%s%s: %v\n", indent, prefix, t.label, t.value)
	t.left.prettyPrint(indent+"  ", "L:")
	t.right.prettyPrint(indent+"  ", "R:")
}

// set inserts the provided label and value into the tree.
func (t *tree[T]) set(l label, value T) {
	if t.label == l {
		t.value = value
		t.hasValue = true
		return
	}

	// TODO: after adding an 'offset' field to label, I thinkk we can simplify
	// this to just use isBitZero. We might even be able to do that now.
	if t.label.isPrefixOf(l) {
		// t.label is a prefix of the new label, so recurse into the
		// appropriate child of n (or create it).
		var next **tree[T]
		// t.label.len < l.len because t.label is strictly a prefix of l
		if zero, _ := l.isBitZero(t.label.len); zero {
			next = &t.left
		} else {
			next = &t.right
		}
		if *next == nil {
			*next = newTree[T](l.rest(t.label.len)).withValue(value)
		} else {
			(*next).set(l.rest(t.label.len), value)
		}
	} else {
		common := t.label.commonPrefixLen(l)

		// Split t and create two new children: an "heir" to inherit t's
		// suffix, and a sibling to handle the new suffix.
		heir := newTree[T](t.label.rest(common)).moveValueFrom(t).moveChildrenFrom(t)
		sibling := newTree[T](l.rest(common)).withValue(value)

		// The bit after the common prefix determines which child will handle
		// which suffix.
		// TODO check ok
		if zero, _ := t.label.isBitZero(common); zero {
			t.left = heir
			t.right = sibling
		} else {
			t.left = sibling
			t.right = heir
		}

		// t's label needs to be truncated at the split point
		t.label = t.label.truncated(common)
	}
}

// remove removes the exact label provided from the tree, if it exists.
func (t *tree[T]) remove(l label) *tree[T] {
	if t == nil {
		return nil
	}

	if l == t.label {
		if t.hasValue {
			t.clearValue()
		}
		switch {
		case t.left == nil && t.right == nil:
			return nil
		case t.left == nil:
			t.right.label = t.label.concat(t.right.label)
			return t.right
		case t.right == nil:
			t.left.label = t.label.concat(t.left.label)
			return t.left
		default:
			return t
		}
	}

	if t.label.isPrefixOf(l) {
		// t.label is a prefix of the new label, so recurse into the
		// appropriate child of t.
		if zero, _ := l.isBitZero(t.label.len); zero {
			t.left = t.left.remove(l.rest(t.label.len))
		} else {
			t.right = t.right.remove(l.rest(t.label.len))
		}
	}

	return t
}

// walkPath traverses the tree starting at this tree's root, following the
// provided path and calling fn at each visited node.
//
// The arguments to fn are (1) a label containing the prefix accumulated during
// the traversal, including the current node, and (2) a pointer to the current
// node.
//
// The return value of fn is a boolean indicating whether traversal should
// stop.
//
// If path is the zero label, all descendants of this tree are visited.
func (t *tree[T]) walk(
	path label,
	pre label,
	fn func(label, *tree[T]) bool,
) {
	if t == nil {
		return
	}

	// Never call fn on root node
	if !t.isZero() {
		if fn(pre.concat(t.label), t) {
			return
		}
	}

	nextPath := path.rest(t.label.len)
	nextPre := pre.concat(t.label)
	zero, pathExhausted := path.isBitZero(t.label.commonPrefixLen(path))

	// Visit the child that matches the next bit in the path. If the path is
	// exhausted, visit both children.
	if zero || !pathExhausted {
		t.left.walk(nextPath, nextPre, fn)
	}
	if !zero || !pathExhausted {
		t.right.walk(nextPath, nextPre, fn)
	}
	return
}

// get returns the value associated with the exact label provided, if it exists.
func (t *tree[T]) get(l label) (val T, ok bool) {
	t.walk(l, label{}, func(key label, m *tree[T]) bool {
		if key == l && m.hasValue {
			val, ok = m.value, true
			return true
		}
		return false
	})
	return
}

// contains returns true if this tree includes the exact label provided.
func (t *tree[T]) contains(l label) (ret bool) {
	t.walk(l, label{}, func(key label, m *tree[T]) bool {
		if ret = (key == l && m.hasValue); ret {
			return true
		}
		return false
	})
	return
}

// encompasses returns true if this tree includes a label which completely
// encompasses the provided label.
func (t *tree[T]) encompasses(l label, strict bool) (ret bool) {
	t.walk(l, label{}, func(key label, m *tree[T]) bool {
		if ret = (key.isPrefixOf(l) && !(strict && key == l) && m.hasValue); ret {
			return true
		}
		return false
	})
	return
}

// covers returns true if this tree includes a subset of labels that completely
// cover the provided label.
func (t *tree[T]) covers(l label, strict bool) (ret bool) {
	// TODO implement
	panic("not implemented")
}

// rootOf returns the shortest-prefix ancestor of the label provided, if any.
// If strict == true, the label itself is not considered.
func (t *tree[T]) rootOf(l label, strict bool) (outKey label, val T, ok bool) {
	t.walk(l, label{}, func(key label, m *tree[T]) bool {
		if key.isPrefixOf(l) && !(strict && key == l) && m.hasValue {
			outKey, val, ok = key, m.value, true
			return true
		}
		return false
	})
	return
}

// parentOf returns the longest-prefix ancestor of the label provided, if any.
// If strict is true, the label itself is not considered.
func (t *tree[T]) parentOf(l label, strict bool) (outKey label, val T, ok bool) {
	t.walk(l, label{}, func(key label, m *tree[T]) bool {
		if key.isPrefixOf(l) && !(strict && key == l) && m.hasValue {
			outKey, val, ok = key, m.value, true
		}
		return false
	})
	return
}

// walkDescendants calls fn on each descendant of the provided label, including
// itself unless strict.
func (t *tree[T]) walkDescendants(l label, strict bool, fn func(label, *tree[T])) {
	t.walk(l, label{}, func(key label, m *tree[T]) bool {
		if l.isPrefixOf(key) && !(strict && key == l) && m.hasValue {
			fn(key, m)
		}
		return false
	})
}

// walkAncestors calls fn on each ancestor of the provided label, including
// itself unless strict.
func (t *tree[T]) walkAncestors(l label, strict bool, fn func(label, *tree[T])) {
	t.walk(l, label{}, func(key label, m *tree[T]) bool {
		if key.isPrefixOf(l) && !(strict && key == l) && m.hasValue {
			fn(key, m)
		}
		return false
	})
}

// filter updates t to include only the labels encompassed by b.
// TODO: I think this can be done more efficiently by walking t and b
// at the same time.
func (t *tree[T]) filter(b *tree[T]) {
	labelsToRemove := make([]label, 0)
	t.walk(label{}, label{}, func(key label, m *tree[T]) bool {
		if !b.encompasses(key, false) {
			labelsToRemove = append(labelsToRemove, key)
		}
		return false
	})
	for _, l := range labelsToRemove {
		fmt.Println("removing", l)
		t.remove(l)
	}
}