// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// gccgo implementation of the weak-pointer runtime hooks used by the
// internal/weak package (and, through it, the unique package).
//
// gccgo's garbage collector does not support true weak references, so a weak
// pointer here keeps its referent alive: the "weak handle" is simply the
// object pointer itself.  This is functionally correct -- a weak.Pointer
// always resolves back to the original object and comparisons behave as
// specified -- at the cost of never reclaiming weakly-referenced objects
// (e.g. values interned by the unique package are not freed).

package runtime

import "unsafe"

// registerWeakPointer returns a weak handle for the object at p.  With no GC
// support for weak references, the handle is p itself.
//
//go:linkname registerWeakPointer runtime.registerWeakPointer
func registerWeakPointer(p unsafe.Pointer) unsafe.Pointer {
	return p
}

// makeStrongFromWeak converts a weak handle back into a strong pointer.  Since
// the handle is the object pointer itself, this is the identity.
//
//go:linkname makeStrongFromWeak runtime.makeStrongFromWeak
func makeStrongFromWeak(p unsafe.Pointer) unsafe.Pointer {
	return p
}
