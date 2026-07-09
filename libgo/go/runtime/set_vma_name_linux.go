// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux

package runtime

import (
	"internal/runtime/atomic"
	"unsafe"
)

// For gccgo the runtime uses its own libc-based syscall wrapper (see
// stubs.go's syscall function), so we cannot import
// internal/runtime/syscall here: that package would be imported under the
// name "syscall", conflicting with the runtime's global syscall function.
// Define the prctl arguments locally instead.
const (
	_PR_SET_VMA           = 0x53564d41
	_PR_SET_VMA_ANON_NAME = 0
)

var prSetVMAUnsupported atomic.Bool

// setVMAName calls prctl(PR_SET_VMA, PR_SET_VMA_ANON_NAME, start, len, name)
func setVMAName(start unsafe.Pointer, length uintptr, name string) {
	if debug.decoratemappings == 0 || prSetVMAUnsupported.Load() {
		return
	}

	var sysName [80]byte
	n := copy(sysName[:], " Go: ")
	copy(sysName[n:79], name) // leave final byte zero

	r := syscall(_SYS_prctl, _PR_SET_VMA, _PR_SET_VMA_ANON_NAME, uintptr(start), length, uintptr(unsafe.Pointer(&sysName[0])), 0)
	if int32(r) == -_EINVAL {
		prSetVMAUnsupported.Store(true)
	}
	// ignore other errors
}
