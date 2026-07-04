// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package unix

<<<<<<< go/./internal/syscall/unix/at_sysnum_netbsd.go
const AT_REMOVEDIR = 0x800
const AT_SYMLINK_NOFOLLOW = 0x200
=======
import "syscall"

const unlinkatTrap uintptr = syscall.SYS_UNLINKAT
const openatTrap uintptr = syscall.SYS_OPENAT
const fstatatTrap uintptr = syscall.SYS_FSTATAT

const (
	AT_EACCESS          = 0x100
	AT_FDCWD            = -0x64
	AT_REMOVEDIR        = 0x800
	AT_SYMLINK_NOFOLLOW = 0x200
>>>>>>> /tmp/go122/src/./internal/syscall/unix/at_sysnum_netbsd.go

	UTIME_OMIT = (1 << 30) - 2
)
