// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package syscall provides the syscall primitives required for the runtime.
package syscall

import (
	"unsafe"
)

// TODO(https://go.dev/issue/51087): This package is incomplete and currently
// only contains very minimal support for Linux.

//extern __go_syscall6
func syscall6(num uintptr, a1, a2, a3, a4, a5, a6 uintptr) uintptr

func getErrno() uintptr
func setErrno(uintptr)

// Syscall6 calls system call number 'num' with arguments a1-6.
func Syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, errno uintptr) {
	setErrno(0)
	r := syscall6(num, a1, a2, a3, a4, a5, a6)
	errno = getErrno()
	return r, 0, errno
}

// Note: in gc, syscall_RawSyscall6 is a push linkname that exports Syscall6
// as syscall.RawSyscall6.  gccgo's syscall package already provides its own
// syscall.RawSyscall6 (syscall/syscall_funcs.go), so that wrapper is omitted
// here to avoid a duplicate definition of the syscall.RawSyscall6 symbol.

func EpollCreate1(flags int32) (fd int32, errno uintptr) {
	r1, _, e := Syscall6(SYS_EPOLL_CREATE1, uintptr(flags), 0, 0, 0, 0, 0)
	return int32(r1), e
}

var _zero uintptr

func EpollWait(epfd int32, events []EpollEvent, maxev, waitms int32) (n int32, errno uintptr) {
	var ev unsafe.Pointer
	if len(events) > 0 {
		ev = unsafe.Pointer(&events[0])
	} else {
		ev = unsafe.Pointer(&_zero)
	}
	r1, _, e := Syscall6(SYS_EPOLL_PWAIT, uintptr(epfd), uintptr(ev), uintptr(maxev), uintptr(waitms), 0, 0)
	return int32(r1), e
}

func EpollCtl(epfd, op, fd int32, event *EpollEvent) (errno uintptr) {
	_, _, e := Syscall6(SYS_EPOLL_CTL, uintptr(epfd), uintptr(op), uintptr(fd), uintptr(unsafe.Pointer(event)), 0, 0)
	return e
}
