// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// gccgo does not support pidfd-based process management: gccgo's syscall
// package has no SysProcAttr.PidFD field and the underlying pidfd_open /
// pidfd_send_signal / waitid(P_PIDFD) plumbing (internal/syscall/unix) is not
// available.  This stub therefore reports that pidfd is unsupported, so the os
// package falls back to the classic PID-based process operations (pidWait /
// signal via the PID).  It mirrors pidfd_other.go, which handles the non-Linux
// platforms.

package os

import "syscall"

func ensurePidfd(sysAttr *syscall.SysProcAttr) (*syscall.SysProcAttr, bool) {
	return sysAttr, false
}

func getPidfd(_ *syscall.SysProcAttr, _ bool) (uintptr, bool) {
	return 0, false
}

func pidfdFind(_ int) (uintptr, error) {
	return 0, syscall.ENOSYS
}

func (p *Process) pidfdRelease() {}

func (_ *Process) pidfdWait() (*ProcessState, error) {
	panic("unreachable")
}

func (_ *Process) pidfdSendSignal(_ syscall.Signal) error {
	panic("unreachable")
}
