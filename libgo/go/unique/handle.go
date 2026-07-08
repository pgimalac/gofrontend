// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package unique

import (
	"sync"
)

// This is a gccgo-specific implementation of the unique package.
//
// The upstream implementation of unique.Make is a very deeply nested generic
// function: it instantiates uniqueMap[T] (which embeds the generic type
// concurrent.HashTrieMap[T, weak.Pointer[T]] -- a generic type parameterized
// by *another* generic type), and calls weak.Make[T], clone[T] and friends.
// gccgo's generics support cannot re-parse/instantiate that particular nest of
// mutually-referential cross-package generic types and functions, so this file
// provides a functionally-equivalent but much simpler implementation.
//
// Values are interned in a single global sync.Map keyed by the value boxed in
// an interface.  Because the constraint is comparable, the boxed value is a
// valid, hashable map key, and interface equality already distinguishes values
// of different dynamic types, so a single map suffices.  As with gccgo's weak
// package (see runtime/weak_gccgo.go), there is no weak-pointer support and
// therefore no reclamation of interned values; this matches the weak==strong
// tradeoff already made elsewhere in gccgo's runtime.  The observable semantics
// of Make and Handle are unchanged: two handles are equal exactly when the
// values used to create them are equal.

// Handle is a globally unique identity for some value of type T.
//
// Two handles compare equal exactly if the two values used to create the handles
// would have also compared equal. The comparison of two handles is trivial and
// typically much more efficient than comparing the values used to create them.
type Handle[T comparable] struct {
	value *T
}

// Value returns a shallow copy of the T value that produced the Handle.
func (h Handle[T]) Value() T {
	return *h.value
}

// uniqueMap interns values of every type in a single map, keyed by the value
// boxed in an interface.  The stored element is the canonical *T for that
// value (also boxed in an interface).
var uniqueMap sync.Map // any(value) -> any(*T)

// Make returns a globally unique handle for a value of type T. Handles
// are equal if and only if the values used to produce them are equal.
func Make[T comparable](value T) Handle[T] {
	if p, ok := uniqueMap.Load(any(value)); ok {
		return Handle[T]{p.(*T)}
	}
	p := new(T)
	*p = value
	actual, _ := uniqueMap.LoadOrStore(any(value), p)
	return Handle[T]{actual.(*T)}
}
