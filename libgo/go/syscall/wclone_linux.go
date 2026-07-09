// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && !loong64

package syscall

// WCLONE (__WCLONE) is the wait flag for "clone" children. It is a fixed
// architecture-independent value on Linux (bit 31). gccgo's generated
// per-arch zerrors only define it for loong64, so define it here for the
// other Linux architectures. exec_linux.go uses it for pidfd waits.
const WCLONE = 0x80000000
