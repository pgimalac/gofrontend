// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (mips64 || mips64le)

package runtime

import "internal/cpu"

func archauxv(tag, val uintptr) {
	switch tag {
	case _AT_HWCAP:
		cpu.HWCap = uint(val)
	}
}
// osArchInit, cputicks, sigset, and the sigset helpers are provided by
// gccgo's shared runtime (signal_gccgo.go) and C support code, matching the
// other gccgo os_linux_*.go arch files, so they are omitted here.
