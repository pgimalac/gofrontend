// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// gccgo-specific runtime hooks for the internal/sync package (new in Go
// 1.24).  These mirror the corresponding sync.runtime_* helpers, exporting
// them under the internal/sync package's linkname so that internal/sync can
// use the same primitives.  (internal/sync.throw and internal/sync.fatal are
// exported from panic.go.)

package runtime

import _ "unsafe" // for go:linkname

//go:linkname internal_sync_runtime_SemacquireMutex internal_1sync.runtime__SemacquireMutex
func internal_sync_runtime_SemacquireMutex(addr *uint32, lifo bool, skipframes int) {
	semacquire1(addr, lifo, semaBlockProfile|semaMutexProfile, skipframes, waitReasonSyncMutexLock)
}

//go:linkname internal_sync_runtime_Semrelease internal_1sync.runtime__Semrelease
func internal_sync_runtime_Semrelease(addr *uint32, handoff bool, skipframes int) {
	semrelease1(addr, handoff, skipframes)
}

//go:linkname internal_sync_runtime_canSpin internal_1sync.runtime__canSpin
func internal_sync_runtime_canSpin(i int) bool {
	return sync_runtime_canSpin(i)
}

//go:linkname internal_sync_runtime_doSpin internal_1sync.runtime__doSpin
func internal_sync_runtime_doSpin() {
	sync_runtime_doSpin()
}

//go:linkname internal_sync_runtime_nanotime internal_1sync.runtime__nanotime
func internal_sync_runtime_nanotime() int64 {
	return nanotime()
}
