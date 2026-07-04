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
	// Call miniterrno so that we can safely make system calls
	// before calling minit on m0.
	miniterrno()

	ncpu = int32(sysconf(__SC_NPROCESSORS_ONLN))
<<<<<<< go/./runtime/os_aix.go
	physPageSize = uintptr(sysconf(__SC_PAGE_SIZE))
=======
	physPageSize = sysconf(__SC_PAGE_SIZE)
}

// newosproc0 is a version of newosproc that can be called before the runtime
// is initialized.
//
// This function is not safe to use after initialization as it does not pass an M as fnarg.
//
//go:nosplit
func newosproc0(stacksize uintptr, fn *funcDescriptor) {
	var (
		attr pthread_attr
		oset sigset
		tid  pthread
	)

	if pthread_attr_init(&attr) != 0 {
		writeErrStr(failthreadcreate)
		exit(1)
	}

	if pthread_attr_setstacksize(&attr, threadStackSize) != 0 {
		writeErrStr(failthreadcreate)
		exit(1)
	}

	if pthread_attr_setdetachstate(&attr, _PTHREAD_CREATE_DETACHED) != 0 {
		writeErrStr(failthreadcreate)
		exit(1)
	}

	// Disable signals during create, so that the new thread starts
	// with signals disabled. It will enable them in minit.
	sigprocmask(_SIG_SETMASK, &sigset_all, &oset)
	var ret int32
	for tries := 0; tries < 20; tries++ {
		// pthread_create can fail with EAGAIN for no reasons
		// but it will be ok if it retries.
		ret = pthread_create(&tid, &attr, fn, nil)
		if ret != _EAGAIN {
			break
		}
		usleep(uint32(tries+1) * 1000) // Milliseconds.
	}
	sigprocmask(_SIG_SETMASK, &oset, nil)
	if ret != 0 {
		writeErrStr(failthreadcreate)
		exit(1)
	}

}

// Called to do synchronous initialization of Go code built with
// -buildmode=c-archive or -buildmode=c-shared.
// None of the Go runtime is initialized.
//
//go:nosplit
//go:nowritebarrierrec
func libpreinit() {
	initsig(true)
}

// Ms related functions
func mpreinit(mp *m) {
	mp.gsignal = malg(32 * 1024) // AIX wants >= 8K
	mp.gsignal.m = mp
}

// errno address must be retrieved by calling _Errno libc function.
// This will return a pointer to errno.
func miniterrno() {
	mp := getg().m
	r, _ := syscall0(&libc__Errno)
	mp.perrno = r

}

func minit() {
	miniterrno()
	minitSignals()
	getg().m.procid = uint64(pthread_self())
}

func unminit() {
	unminitSignals()
	getg().m.procid = 0
}

// Called from exitm, but not from drop, to undo the effect of thread-owned
// resources in minit, semacreate, or elsewhere. Do not take locks after calling this.
func mdestroy(mp *m) {
}

// tstart is a function descriptor to _tstart defined in assembly.
var tstart funcDescriptor

func newosproc(mp *m) {
	var (
		attr pthread_attr
		oset sigset
		tid  pthread
	)

	if pthread_attr_init(&attr) != 0 {
		throw("pthread_attr_init")
	}

	if pthread_attr_setstacksize(&attr, threadStackSize) != 0 {
		throw("pthread_attr_getstacksize")
	}

	if pthread_attr_setdetachstate(&attr, _PTHREAD_CREATE_DETACHED) != 0 {
		throw("pthread_attr_setdetachstate")
	}

	// Disable signals during create, so that the new thread starts
	// with signals disabled. It will enable them in minit.
	sigprocmask(_SIG_SETMASK, &sigset_all, &oset)
	ret := retryOnEAGAIN(func() int32 {
		return pthread_create(&tid, &attr, &tstart, unsafe.Pointer(mp))
	})
	sigprocmask(_SIG_SETMASK, &oset, nil)
	if ret != 0 {
		print("runtime: failed to create new OS thread (have ", mcount(), " already; errno=", ret, ")\n")
		if ret == _EAGAIN {
			println("runtime: may need to increase max user processes (ulimit -u)")
		}
		throw("newosproc")
	}

}

func exitThread(wait *atomic.Uint32) {
	// We should never reach exitThread on AIX because we let
	// libc clean up threads.
	throw("exitThread")
}

var urandom_dev = []byte("/dev/urandom\x00")

//go:nosplit
func readRandom(r []byte) int {
	fd := open(&urandom_dev[0], 0 /* O_RDONLY */, 0)
	n := read(fd, unsafe.Pointer(&r[0]), int32(len(r)))
	closefd(fd)
	return int(n)
}

func goenvs() {
	goenvs_unix()
}

/* SIGNAL */

const (
	_NSIG = 256
)

// sigtramp is a function descriptor to _sigtramp defined in assembly
var sigtramp funcDescriptor

//go:nosplit
//go:nowritebarrierrec
func setsig(i uint32, fn uintptr) {
	var sa sigactiont
	sa.sa_flags = _SA_SIGINFO | _SA_ONSTACK | _SA_RESTART
	sa.sa_mask = sigset_all
	if fn == abi.FuncPCABIInternal(sighandler) { // abi.FuncPCABIInternal(sighandler) matches the callers in signal_unix.go
		fn = uintptr(unsafe.Pointer(&sigtramp))
	}
	sa.sa_handler = fn
	sigaction(uintptr(i), &sa, nil)

}

//go:nosplit
//go:nowritebarrierrec
func setsigstack(i uint32) {
	var sa sigactiont
	sigaction(uintptr(i), nil, &sa)
	if sa.sa_flags&_SA_ONSTACK != 0 {
		return
	}
	sa.sa_flags |= _SA_ONSTACK
	sigaction(uintptr(i), &sa, nil)
}

//go:nosplit
//go:nowritebarrierrec
func getsig(i uint32) uintptr {
	var sa sigactiont
	sigaction(uintptr(i), nil, &sa)
	return sa.sa_handler
}

// setSignalstackSP sets the ss_sp field of a stackt.
//
//go:nosplit
func setSignalstackSP(s *stackt, sp uintptr) {
	*(*uintptr)(unsafe.Pointer(&s.ss_sp)) = sp
}

//go:nosplit
func (c *sigctxt) fixsigcode(sig uint32) {
	switch sig {
	case _SIGPIPE:
		// For SIGPIPE, c.sigcode() isn't set to _SI_USER as on Linux.
		// Therefore, raisebadsignal won't raise SIGPIPE again if
		// it was deliver in a non-Go thread.
		c.set_sigcode(_SI_USER)
	}
}

//go:nosplit
//go:nowritebarrierrec
func sigaddset(mask *sigset, i int) {
	(*mask)[(i-1)/64] |= 1 << ((uint32(i) - 1) & 63)
}

func sigdelset(mask *sigset, i int) {
	(*mask)[(i-1)/64] &^= 1 << ((uint32(i) - 1) & 63)
>>>>>>> /tmp/go122/src/./runtime/os_aix.go
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
// sigPerThreadSyscall is only used on linux, so we assign a bogus signal
// number.
const sigPerThreadSyscall = 1 << 31

//go:nosplit
func runPerThreadSyscall() {
	throw("runPerThreadSyscall only valid on linux")
}
