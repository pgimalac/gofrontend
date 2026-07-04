// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/abi"
	"internal/goarch"
	"runtime/internal/atomic"
	"unsafe"
)

type mOS struct {
	waitsemacount uint32
}

func getProcID() uint64 {
	return uint64(lwp_self())
}

//extern-sysinfo _lwp_self
func lwp_self() int32

//go:noescape
//extern-sysinfo _lwp_park
func lwp_park(ts int32, rel int32, abstime *timespec, unpark int32, hint, unparkhint unsafe.Pointer) int32

//go:noescape
//extern-sysinfo _lwp_unpark
func lwp_unpark(lwp int32, hint unsafe.Pointer) int32

//go:noescape
<<<<<<< go/./runtime/os_netbsd.go
//extern-sysinfo sysctl
func sysctl(*uint32, uint32, *byte, *uintptr, *byte, uintptr) int32
=======
func kevent(kq int32, ch *keventt, nch int32, ev *keventt, nev int32, ts *timespec) int32

func pipe2(flags int32) (r, w int32, errno int32)
func fcntl(fd, cmd, arg int32) (ret int32, errno int32)

func issetugid() int32

const (
	_ESRCH     = 3
	_ETIMEDOUT = 60

	// From NetBSD's <sys/time.h>
	_CLOCK_REALTIME  = 0
	_CLOCK_VIRTUAL   = 1
	_CLOCK_PROF      = 2
	_CLOCK_MONOTONIC = 3

	_TIMER_RELTIME = 0
	_TIMER_ABSTIME = 1
)

var sigset_all = sigset{[4]uint32{^uint32(0), ^uint32(0), ^uint32(0), ^uint32(0)}}

// From NetBSD's <sys/sysctl.h>
const (
	_CTL_KERN   = 1
	_KERN_OSREV = 3

	_CTL_HW        = 6
	_HW_NCPU       = 3
	_HW_PAGESIZE   = 7
	_HW_NCPUONLINE = 16
)
>>>>>>> /tmp/go121/src/./runtime/os_netbsd.go

func sysctlInt(mib []uint32) (int32, bool) {
	var out int32
	nout := unsafe.Sizeof(out)
	ret := sysctl(&mib[0], uint32(len(mib)), (*byte)(unsafe.Pointer(&out)), &nout, nil, 0)
	if ret < 0 {
		return 0, false
	}
	return out, true
}

func getncpu() int32 {
	if n, ok := sysctlInt([]uint32{_CTL_HW, _HW_NCPUONLINE}); ok {
		return int32(n)
	}
	if n, ok := sysctlInt([]uint32{_CTL_HW, _HW_NCPU}); ok {
		return int32(n)
	}
	return 1
}

func getPageSize() uintptr {
	mib := [2]uint32{_CTL_HW, _HW_PAGESIZE}
	out := uint32(0)
	nout := unsafe.Sizeof(out)
	ret := sysctl(&mib[0], 2, (*byte)(unsafe.Pointer(&out)), &nout, nil, 0)
	if ret >= 0 {
		return uintptr(out)
	}
	return 0
}

func getOSRev() int {
	if osrev, ok := sysctlInt([]uint32{_CTL_KERN, _KERN_OSREV}); ok {
		return int(osrev)
	}
	return 0
}

//go:nosplit
func semacreate(mp *m) {
}

//go:nosplit
func semasleep(ns int64) int32 {
	gp := getg()
	var deadline int64
	if ns >= 0 {
		deadline = nanotime() + ns
	}

	for {
		v := atomic.Load(&gp.m.waitsemacount)
		if v > 0 {
			if atomic.Cas(&gp.m.waitsemacount, v, v-1) {
				return 0 // semaphore acquired
			}
			continue
		}

		// Sleep until unparked by semawakeup or timeout.
		var tsp *timespec
		var ts timespec
		if ns >= 0 {
			wait := deadline - nanotime()
			if wait <= 0 {
				return -1
			}
			ts.setNsec(wait)
			tsp = &ts
		}
		ret := lwp_park(_CLOCK_MONOTONIC, _TIMER_RELTIME, tsp, 0, unsafe.Pointer(&_g_.m.waitsemacount), nil)
		if ret != 0 && errno() == _ETIMEDOUT {
			return -1
		}
	}
}

//go:nosplit
func semawakeup(mp *m) {
	atomic.Xadd(&mp.waitsemacount, 1)
	// From NetBSD's _lwp_unpark(2) manual:
	// "If the target LWP is not currently waiting, it will return
	// immediately upon the next call to _lwp_park()."
	ret := lwp_unpark(int32(mp.procid), unsafe.Pointer(&mp.waitsemacount))
	if ret != 0 && errno() != _ESRCH {
		// semawakeup can be called on signal stack.
		systemstack(func() {
			print("thrwakeup addr=", &mp.waitsemacount, " sem=", mp.waitsemacount, " errno=", errno(), "\n")
		})
	}
}

<<<<<<< go/./runtime/os_netbsd.go
=======
// May run with m.p==nil, so write barriers are not allowed.
//
//go:nowritebarrier
func newosproc(mp *m) {
	stk := unsafe.Pointer(mp.g0.stack.hi)
	if false {
		print("newosproc stk=", stk, " m=", mp, " g=", mp.g0, " id=", mp.id, " ostk=", &mp, "\n")
	}

	var uc ucontextt
	getcontext(unsafe.Pointer(&uc))

	// _UC_SIGMASK does not seem to work here.
	// It would be nice if _UC_SIGMASK and _UC_STACK
	// worked so that we could do all the work setting
	// the sigmask and the stack here, instead of setting
	// the mask here and the stack in netbsdMstart.
	// For now do the blocking manually.
	uc.uc_flags = _UC_SIGMASK | _UC_CPU
	uc.uc_link = nil
	uc.uc_sigmask = sigset_all

	var oset sigset
	sigprocmask(_SIG_SETMASK, &sigset_all, &oset)

	lwp_mcontext_init(&uc.uc_mcontext, stk, mp, mp.g0, abi.FuncPCABI0(netbsdMstart))

	ret := retryOnEAGAIN(func() int32 {
		errno := lwp_create(unsafe.Pointer(&uc), _LWP_DETACHED, unsafe.Pointer(&mp.procid))
		// lwp_create returns negative errno
		return -errno
	})
	sigprocmask(_SIG_SETMASK, &oset, nil)
	if ret != 0 {
		print("runtime: failed to create new OS thread (have ", mcount()-1, " already; errno=", ret, ")\n")
		if ret == _EAGAIN {
			println("runtime: may need to increase max user processes (ulimit -p)")
		}
		throw("runtime.newosproc")
	}
}

// mstart is the entry-point for new Ms.
// It is written in assembly, uses ABI0, is marked TOPFRAME, and calls netbsdMstart0.
func netbsdMstart()

// netbsdMstart0 is the function call that starts executing a newly
// created thread. On NetBSD, a new thread inherits the signal stack
// of the creating thread. That confuses minit, so we remove that
// signal stack here before calling the regular mstart. It's a bit
// baroque to remove a signal stack here only to add one in minit, but
// it's a simple change that keeps NetBSD working like other OS's.
// At this point all signals are blocked, so there is no race.
//
//go:nosplit
func netbsdMstart0() {
	st := stackt{ss_flags: _SS_DISABLE}
	sigaltstack(&st, nil)
	mstart0()
}

>>>>>>> /tmp/go121/src/./runtime/os_netbsd.go
func osinit() {
	ncpu = getncpu()
	if physPageSize == 0 {
		physPageSize = getPageSize()
	}
	needSysmonWorkaround = getOSRev() < 902000000 // NetBSD 9.2
}

func sysargs(argc int32, argv **byte) {
	n := argc + 1

	// skip over argv, envp to get to auxv
	for argv_index(argv, n) != nil {
		n++
	}

	// skip NULL separator
	n++

	// now argv+n is auxv
	auxvp := (*[1 << 28]uintptr)(add(unsafe.Pointer(argv), uintptr(n)*goarch.PtrSize))
	pairs := sysauxv(auxvp[:])
	auxv = auxvp[: pairs*2 : pairs*2]
}

const (
	_AT_NULL   = 0 // Terminates the vector
	_AT_PAGESZ = 6 // Page size in bytes
)

func sysauxv(auxv []uintptr) (pairs int) {
	var i int
	for i = 0; auxv[i] != _AT_NULL; i += 2 {
		tag, val := auxv[i], auxv[i+1]
		switch tag {
		case _AT_PAGESZ:
			physPageSize = val
		}
	}
	return i / 2
}
