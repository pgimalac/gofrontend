// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"

type mOS struct {
	waitsema uintptr // semaphore for parking on locks
}

func getProcID() uint64 {
	return uint64(gettid())
}

//extern malloc
func libc_malloc(uintptr) unsafe.Pointer

<<<<<<< go/./runtime/os_solaris.go
//go:noescape
//extern sem_init
func sem_init(sem *semt, pshared int32, value uint32) int32

//go:noescape
//extern sem_wait
func sem_wait(sem *semt) int32
=======
// sysvicall1Err returns both the system call result and the errno value.
// This is used by sysvicall1 and pipe.
//
//go:nosplit
func sysvicall1Err(fn *libcFunc, a1 uintptr) (r1, err uintptr) {
	// Leave caller's PC/SP around for traceback.
	gp := getg()
	var mp *m
	if gp != nil {
		mp = gp.m
	}
	if mp != nil && mp.libcallsp == 0 {
		mp.libcallg.set(gp)
		mp.libcallpc = getcallerpc()
		// sp must be the last, because once async cpu profiler finds
		// all three values to be non-zero, it will use them
		mp.libcallsp = getcallersp()
	} else {
		mp = nil
	}
>>>>>>> /tmp/go121/src/./runtime/os_solaris.go

//go:noescape
//extern sem_post
func sem_post(sem *semt) int32

//go:noescape
//extern sem_reltimedwait_np
func sem_reltimedwait_np(sem *semt, timeout *timespec) int32

//go:nosplit
func semacreate(mp *m) {
	if mp.waitsema != 0 {
		return
	}

<<<<<<< go/./runtime/os_solaris.go
	var sem *semt
=======
//go:nosplit
func sysvicall3(fn *libcFunc, a1, a2, a3 uintptr) uintptr {
	r1, _ := sysvicall3Err(fn, a1, a2, a3)
	return r1
}

//go:nosplit
//go:cgo_unsafe_args

// sysvicall3Err returns both the system call result and the errno value.
// This is used by sysvicall3 and write1.
func sysvicall3Err(fn *libcFunc, a1, a2, a3 uintptr) (r1, err uintptr) {
	// Leave caller's PC/SP around for traceback.
	gp := getg()
	var mp *m
	if gp != nil {
		mp = gp.m
	}
	if mp != nil && mp.libcallsp == 0 {
		mp.libcallg.set(gp)
		mp.libcallpc = getcallerpc()
		// sp must be the last, because once async cpu profiler finds
		// all three values to be non-zero, it will use them
		mp.libcallsp = getcallersp()
	} else {
		mp = nil
	}
>>>>>>> /tmp/go121/src/./runtime/os_solaris.go

	// Call libc's malloc rather than malloc. This will
	// allocate space on the C heap. We can't call malloc
	// here because it could cause a deadlock.
	sem = (*semt)(libc_malloc(unsafe.Sizeof(*sem)))
	if sem_init(sem, 0, 0) != 0 {
		throw("sem_init")
	}
	mp.waitsema = uintptr(unsafe.Pointer(sem))
}

//go:nosplit
func semasleep(ns int64) int32 {
	_m_ := getg().m
	if ns >= 0 {
		var ts timespec
		ts.setNsec(ns)

		if sem_reltimedwait_np((*semt)(unsafe.Pointer(_m_.waitsema)), &ts) != 0 {
			err := errno()
			if err == _ETIMEDOUT || err == _EAGAIN || err == _EINTR {
				return -1
			}
			throw("sem_reltimedwait_np")
		}
		return 0
	}
	for {
		r1 := sem_wait((*semt)(unsafe.Pointer(_m_.waitsema)))
		if r1 == 0 {
			break
		}
		if errno() == _EINTR {
			continue
		}
		throw("sem_wait")
	}
	return 0
}

//go:nosplit
func semawakeup(mp *m) {
	if sem_post((*semt)(unsafe.Pointer(mp.waitsema))) != 0 {
		throw("sem_post")
	}
}

func issetugid() int32 {
	return int32(sysvicall0(&libc_issetugid))
}
