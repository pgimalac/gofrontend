// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package concurrent

import (
	"sync"
)

// HashTrieMap is a concurrent map.
//
// The upstream implementation is a lock-free concurrent hash-trie built out of
// several unexported generic helper types (indirect, node, entry) that embed
// one another and use atomic.Pointer[node[K, V]] fields.  gccgo's generics
// support does not handle instantiating that particular nest of mutually
// referential unexported generic types across package boundaries, so this
// gccgo-specific version keeps the same public API but is implemented as a
// thin generic wrapper around sync.Map.  It is functionally equivalent (the
// unique package, the only user, only relies on Load/LoadOrStore/
// CompareAndDelete/All semantics) at the cost of the lock-free performance
// characteristics of the upstream trie.
type HashTrieMap[K, V comparable] struct {
	m sync.Map
}

// NewHashTrieMap creates a new HashTrieMap for the provided key and value.
func NewHashTrieMap[K, V comparable]() *HashTrieMap[K, V] {
	return &HashTrieMap[K, V]{}
}

// Load returns the value stored in the map for a key, or the zero value if no
// value is present.
// The ok result indicates whether value was found in the map.
func (ht *HashTrieMap[K, V]) Load(key K) (value V, ok bool) {
	v, ok := ht.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return v.(V), true
}

// LoadOrStore returns the existing value for the key if present. Otherwise, it
// stores and returns the given value. The loaded result is true if the value
// was loaded, false if stored.
func (ht *HashTrieMap[K, V]) LoadOrStore(key K, value V) (result V, loaded bool) {
	v, loaded := ht.m.LoadOrStore(key, value)
	return v.(V), loaded
}

// CompareAndDelete deletes the entry for key if its value is equal to old.
//
// If there is no current value for key in the map, CompareAndDelete returns
// false (even if the old value is the nil interface value).
func (ht *HashTrieMap[K, V]) CompareAndDelete(key K, old V) (deleted bool) {
	return ht.m.CompareAndDelete(key, old)
}

// All returns an iterator over each key and value present in the map.
//
// The iterator does not necessarily correspond to any consistent snapshot of
// the HashTrieMap's contents: no key will be visited more than once, but if
// the value for any key is stored or deleted concurrently, the iterator may
// reflect any mapping for that key from any point during iteration.
func (ht *HashTrieMap[K, V]) All() func(yield func(K, V) bool) {
	return func(yield func(key K, value V) bool) {
		ht.m.Range(func(key, value any) bool {
			return yield(key.(K), value.(V))
		})
	}
}
