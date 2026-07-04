// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build aix

package runtime

import (
	"unsafe"
)

//extern sysconf
func sysconf(int32) _C_long

type mOS struct {
	waitsema uintptr // semaphore for parking on locks
}

func getProcID() uint64 {
	return uint64(gettid())
}

//extern malloc
func libc_malloc(uintptr) unsafe.Pointer

//go:noescape
//extern sem_init
func sem_init(sem *semt, pshared int32, value uint32) int32

//go:noescape
//extern sem_wait
func sem_wait(sem *semt) int32

//go:noescape
//extern sem_post
func sem_post(sem *semt) int32

//go:noescape
//extern sem_timedwait
func sem_timedwait(sem *semt, timeout *timespec) int32

//go:noescape
//extern clock_gettime
func clock_gettime(clock_id int64, timeout *timespec) int32

//go:nosplit
func semacreate(mp *m) {
	if mp.waitsema != 0 {
		return
	}

	var sem *semt

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
	mp := getg().m
	if ns >= 0 {
		var ts timespec

		if clock_gettime(_CLOCK_REALTIME, &ts) != 0 {
			throw("clock_gettime")
		}

		sec := int64(ts.tv_sec) + ns/1e9
		nsec := int64(ts.tv_nsec) + ns%1e9
		if nsec >= 1e9 {
			sec++
			nsec -= 1e9
		}
		if sec != int64(timespec_sec_t(sec)) {
			// Handle overflows (timespec_sec_t is 32-bit in 32-bit applications)
			sec = 1<<31 - 1
		}
		ts.tv_sec = timespec_sec_t(sec)
		ts.tv_nsec = timespec_nsec_t(nsec)

		if sem_timedwait((*semt)(unsafe.Pointer(mp.waitsema)), &ts) != 0 {
			err := errno()
			if err == _ETIMEDOUT || err == _EAGAIN || err == _EINTR {
				return -1
			}
			println("sem_timedwait err ", err, " ts.tv_sec ", ts.tv_sec, " ts.tv_nsec ", ts.tv_nsec, " ns ", ns, " id ", mp.id)
			throw("sem_timedwait")
		}
		return 0
	}
	for {
		r1 := sem_wait((*semt)(unsafe.Pointer(mp.waitsema)))
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

func osinit() {
	ncpu = int32(sysconf(__SC_NPROCESSORS_ONLN))
	physPageSize = uintptr(sysconf(__SC_PAGE_SIZE))
}

func setProcessCPUProfiler(hz int32) {
	setProcessCPUProfilerTimer(hz)
}

func setThreadCPUProfiler(hz int32) {
	setThreadCPUProfilerHz(hz)
}

//go:nosplit
func validSIGPROF(mp *m, c *sigctxt) bool {
	return true
}

const (
	_CLOCK_REALTIME  = 9
	_CLOCK_MONOTONIC = 10
)
<<<<<<< go/./runtime/os_aix.go
=======

//go:nosplit
func nanotime1() int64 {
	tp := &timespec{}
	if clock_gettime(_CLOCK_REALTIME, tp) != 0 {
		throw("syscall clock_gettime failed")
	}
	return tp.tv_sec*1000000000 + tp.tv_nsec
}

func walltime() (sec int64, nsec int32) {
	ts := &timespec{}
	if clock_gettime(_CLOCK_REALTIME, ts) != 0 {
		throw("syscall clock_gettime failed")
	}
	return ts.tv_sec, int32(ts.tv_nsec)
}

//go:nosplit
func fcntl(fd, cmd, arg int32) (int32, int32) {
	r, errno := syscall3(&libc_fcntl, uintptr(fd), uintptr(cmd), uintptr(arg))
	return int32(r), int32(errno)
}

//go:nosplit
func setNonblock(fd int32) {
	flags, _ := fcntl(fd, _F_GETFL, 0)
	if flags != -1 {
		fcntl(fd, _F_SETFL, flags|_O_NONBLOCK)
	}
}

// sigPerThreadSyscall is only used on linux, so we assign a bogus signal
// number.
const sigPerThreadSyscall = 1 << 31

//go:nosplit
func runPerThreadSyscall() {
	throw("runPerThreadSyscall only valid on linux")
}

//go:nosplit
func getuid() int32 {
	r, errno := syscall0(&libc_getuid)
	if errno != 0 {
		print("getuid failed ", errno)
		throw("getuid")
	}
	return int32(r)
}

//go:nosplit
func geteuid() int32 {
	r, errno := syscall0(&libc_geteuid)
	if errno != 0 {
		print("geteuid failed ", errno)
		throw("geteuid")
	}
	return int32(r)
}

//go:nosplit
func getgid() int32 {
	r, errno := syscall0(&libc_getgid)
	if errno != 0 {
		print("getgid failed ", errno)
		throw("getgid")
	}
	return int32(r)
}

//go:nosplit
func getegid() int32 {
	r, errno := syscall0(&libc_getegid)
	if errno != 0 {
		print("getegid failed ", errno)
		throw("getegid")
	}
	return int32(r)
}
>>>>>>> /tmp/go121/src/./runtime/os_aix.go
