// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build aix || darwin || dragonfly || freebsd || hurd || linux || netbsd || openbsd || solaris

package syscall

import (
<<<<<<< go/./syscall/syscall_unix.go
=======
	errorspkg "errors"
	"internal/bytealg"
	"internal/itoa"
	"internal/oserror"
>>>>>>> /tmp/go121/src/./syscall/syscall_unix.go
	"internal/race"
	"runtime"
	"sync"
	"unsafe"
)

var (
	Stdin  = 0
	Stdout = 1
	Stderr = 2
)

const (
	darwin64Bit = (runtime.GOOS == "darwin" || runtime.GOOS == "ios") && sizeofPtr == 8
	netbsd32Bit = runtime.GOOS == "netbsd" && sizeofPtr == 4
)

// clen returns the index of the first NULL byte in n or len(n) if n contains no NULL byte.
func clen(n []byte) int {
	for i := 0; i < len(n); i++ {
		if n[i] == 0 {
			return i
		}
	}
	return len(n)
}

// Mmap manager, for use by operating system-specific implementations.
// Gccgo only has one implementation but we do this to correspond to gc.

type mmapper struct {
	sync.Mutex
	active map[*byte][]byte // active mappings; key is last byte in mapping
	mmap   func(addr, length uintptr, prot, flags, fd int, offset int64) (uintptr, error)
	munmap func(addr uintptr, length uintptr) error
}

func (m *mmapper) Mmap(fd int, offset int64, length int, prot int, flags int) (data []byte, err error) {
	if length <= 0 {
		return nil, EINVAL
	}

	// Map the requested memory.
	addr, errno := m.mmap(0, uintptr(length), prot, flags, fd, offset)
	if errno != nil {
		return nil, errno
	}

	// Use unsafe to turn addr into a []byte.
	b := unsafe.Slice((*byte)(unsafe.Pointer(addr)), length)

	// Register mapping in m and return it.
	p := &b[cap(b)-1]
	m.Lock()
	defer m.Unlock()
	m.active[p] = b
	return b, nil
}

func (m *mmapper) Munmap(data []byte) (err error) {
	if len(data) == 0 || len(data) != cap(data) {
		return EINVAL
	}

	// Find the base of the mapping.
	p := &data[cap(data)-1]
	m.Lock()
	defer m.Unlock()
	b := m.active[p]
	if b == nil || &b[0] != &data[0] {
		return EINVAL
	}

	// Unmap the memory and update m.
	if errno := m.munmap(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b))); errno != nil {
		return errno
	}
	delete(m.active, p)
	return nil
}

<<<<<<< go/./syscall/syscall_unix.go
var mapper = &mmapper{
	active: make(map[*byte][]byte),
	mmap:   mmap,
	munmap: munmap,
=======
// An Errno is an unsigned number describing an error condition.
// It implements the error interface. The zero Errno is by convention
// a non-error, so code to convert from Errno to error should use:
//
//	err = nil
//	if errno != 0 {
//		err = errno
//	}
//
// Errno values can be tested against error values using errors.Is.
// For example:
//
//	_, _, err := syscall.Syscall(...)
//	if errors.Is(err, fs.ErrNotExist) ...
type Errno uintptr

func (e Errno) Error() string {
	if 0 <= int(e) && int(e) < len(errors) {
		s := errors[e]
		if s != "" {
			return s
		}
	}
	return "errno " + itoa.Itoa(int(e))
>>>>>>> /tmp/go121/src/./syscall/syscall_unix.go
}

<<<<<<< go/./syscall/syscall_unix.go
func Mmap(fd int, offset int64, length int, prot int, flags int) (data []byte, err error) {
	return mapper.Mmap(fd, offset, length, prot, flags)
=======
func (e Errno) Is(target error) bool {
	switch target {
	case oserror.ErrPermission:
		return e == EACCES || e == EPERM
	case oserror.ErrExist:
		return e == EEXIST || e == ENOTEMPTY
	case oserror.ErrNotExist:
		return e == ENOENT
	case errorspkg.ErrUnsupported:
		return e == ENOSYS || e == ENOTSUP || e == EOPNOTSUPP
	}
	return false
>>>>>>> /tmp/go121/src/./syscall/syscall_unix.go
}

func Munmap(b []byte) (err error) {
	return mapper.Munmap(b)
}

// Do the interface allocations only once for common
// Errno values.
var (
	errEAGAIN error = EAGAIN
	errEINVAL error = EINVAL
	errENOENT error = ENOENT
)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e Errno) error {
	switch e {
	case 0:
		return nil
	case EAGAIN:
		return errEAGAIN
	case EINVAL:
		return errEINVAL
	case ENOENT:
		return errENOENT
	}
	return e
}

// A Signal is a number describing a process signal.
// It implements the os.Signal interface.
type Signal int

func (s Signal) Signal() {}

func Signame(s Signal) string

func (s Signal) String() string {
	return Signame(s)
}

func Read(fd int, p []byte) (n int, err error) {
	n, err = read(fd, p)
	if race.Enabled {
		if n > 0 {
			race.WriteRange(unsafe.Pointer(&p[0]), n)
		}
		if err == nil {
			race.Acquire(unsafe.Pointer(&ioSync))
		}
	}
	if msanenabled && n > 0 {
		msanWrite(unsafe.Pointer(&p[0]), n)
	}
	if asanenabled && n > 0 {
		asanWrite(unsafe.Pointer(&p[0]), n)
	}
	return
}

func Write(fd int, p []byte) (n int, err error) {
	if race.Enabled {
		race.ReleaseMerge(unsafe.Pointer(&ioSync))
	}
	if faketime && (fd == 1 || fd == 2) {
		n = faketimeWrite(fd, p)
		if n < 0 {
			n, err = 0, errnoErr(Errno(-n))
		}
	} else {
		n, err = write(fd, p)
	}
	if race.Enabled && n > 0 {
		race.ReadRange(unsafe.Pointer(&p[0]), n)
	}
	if msanenabled && n > 0 {
		msanRead(unsafe.Pointer(&p[0]), n)
	}
	if asanenabled && n > 0 {
		asanRead(unsafe.Pointer(&p[0]), n)
	}
	return
}

var ioSync int64
