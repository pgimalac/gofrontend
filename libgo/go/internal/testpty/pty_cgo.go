// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build cgo && (aix || dragonfly || freebsd || hurd || (linux && !android) || netbsd || openbsd || solaris)

package testpty

// This is the gccgo version of the pty support. Rather than using cgo,
// it calls the C library functions directly via gccgo's //extern
// mechanism, so that the libgo test build does not require a C.gox.

import (
	"os"
	"syscall"
	"unsafe"
)

//extern posix_openpt
func posix_openpt(int32) int32

//extern grantpt
func grantpt(int32) int32

//extern unlockpt
func unlockpt(int32) int32

//extern ptsname
func ptsname(int32) *byte

//extern close
func libc_close(int32) int32

const _O_RDWR = 2

func open() (pty *os.File, processTTY string, err error) {
<<<<<<< go/./internal/testpty/pty_cgo.go
	m := posix_openpt(_O_RDWR)
	if m < 0 {
		return nil, "", ptyError("posix_openpt", syscall.GetErrno())
	}
	if grantpt(m) < 0 {
		errno := syscall.GetErrno()
		libc_close(m)
		return nil, "", ptyError("grantpt", errno)
=======
	m, err := C.posix_openpt(C.O_RDWR)
	if m < 0 {
		return nil, "", ptyError("posix_openpt", err)
>>>>>>> /tmp/go121/src/./internal/testpty/pty_cgo.go
	}
<<<<<<< go/./internal/testpty/pty_cgo.go
	if unlockpt(m) < 0 {
		errno := syscall.GetErrno()
		libc_close(m)
		return nil, "", ptyError("unlockpt", errno)
=======
	if res, err := C.grantpt(m); res < 0 {
		C.close(m)
		return nil, "", ptyError("grantpt", err)
>>>>>>> /tmp/go121/src/./internal/testpty/pty_cgo.go
	}
<<<<<<< go/./internal/testpty/pty_cgo.go
	p := ptsname(m)
	s := (*[32000]byte)(unsafe.Pointer(p))[:]
	for i, v := range s {
		if v == 0 {
			s = s[:i:i]
			break
		}
=======
	if res, err := C.unlockpt(m); res < 0 {
		C.close(m)
		return nil, "", ptyError("unlockpt", err)
>>>>>>> /tmp/go121/src/./internal/testpty/pty_cgo.go
	}
	processTTY = string(s)
	return os.NewFile(uintptr(m), "pty"), processTTY, nil
}
