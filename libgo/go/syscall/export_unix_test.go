// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build dragonfly || freebsd || hurd || linux || netbsd || openbsd || solaris

package syscall

<<<<<<< go/./syscall/export_unix_test.go
import "unsafe"

func Ioctl(fd, req uintptr, arg unsafe.Pointer) (err Errno) {
	_, err = raw_ioctl_ptr(int(fd), req, arg)
=======
import "unsafe"

func IoctlPtr(fd, req uintptr, arg unsafe.Pointer) (err Errno) {
	_, _, err = Syscall(SYS_IOCTL, fd, req, uintptr(arg))
>>>>>>> /tmp/go121/src/./syscall/export_unix_test.go
	return err
}
