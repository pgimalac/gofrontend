// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"

// gccgo does not implement the gc runtime's dedicated cleanup machinery
// (mcleanup.go, which relies on gc-specific span specials and cleanup
// goroutines). Instead it implements runtime.AddCleanup on top of the
// existing finalizer mechanism: a cleanup runs when ptr becomes unreachable,
// exactly like a finalizer, with the same restriction that the cleanup
// function and its argument must not reference ptr (otherwise ptr stays
// reachable and the cleanup never runs).
//
// Compared to the gc implementation this has two limitations that do not
// matter for the standard library's uses: only one cleanup/finalizer is
// effective per object (a later AddCleanup or SetFinalizer on the same ptr
// replaces an earlier one), and cleanups share the finalizer goroutine.

// Cleanup is a handle to a cleanup call for a specific object.
type Cleanup struct {
	// ptr is the object carrying the cleanup finalizer, or nil for a no-op
	// cleanup or after Stop. Stored as unsafe.Pointer (not an interface) so
	// this exported type has a complete C representation in runtime.inc.
	ptr unsafe.Pointer
}

// AddCleanup attaches a cleanup function to ptr. The cleanup(arg) call runs
// once ptr is no longer reachable, in a separate goroutine. arg must not be
// (or reference) ptr. See the package documentation for runtime.AddCleanup.
func AddCleanup[T, S any](ptr *T, cleanup func(S), arg S) Cleanup {
	if ptr == nil {
		panic("runtime.AddCleanup: ptr is nil")
	}
	// Guard against arg == ptr, which would keep ptr reachable forever.
	if p, ok := any(arg).(*T); ok && p == ptr {
		panic("runtime.AddCleanup: ptr is equal to arg, cleanup will never run")
	}
	SetFinalizer(ptr, func(*T) {
		cleanup(arg)
	})
	return Cleanup{ptr: unsafe.Pointer(ptr)}
}

// Stop cancels the cleanup call. Stop has no effect if the cleanup call has
// already been queued for execution.
func (c Cleanup) Stop() {
	if c.ptr == nil {
		return
	}
	systemstack(func() {
		removefinalizer(c.ptr)
	})
}
