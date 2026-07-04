// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package unix

<<<<<<< go/./internal/syscall/unix/at_sysnum_dragonfly.go
const AT_REMOVEDIR = 0x2
const AT_SYMLINK_NOFOLLOW = 0x1
=======
import "syscall"

const unlinkatTrap uintptr = syscall.SYS_UNLINKAT
const openatTrap uintptr = syscall.SYS_OPENAT
const fstatatTrap uintptr = syscall.SYS_FSTATAT

const (
	AT_EACCESS          = 0x4
	AT_FDCWD            = 0xfffafdcd
	AT_REMOVEDIR        = 0x2
	AT_SYMLINK_NOFOLLOW = 0x1
>>>>>>> /tmp/go122/src/./internal/syscall/unix/at_sysnum_dragonfly.go

	UTIME_OMIT = -0x1
)
