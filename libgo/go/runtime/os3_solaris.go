// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import _ "unsafe"

<<<<<<< go/./runtime/os3_solaris.go
var executablePath string
=======
func osinit() {
	// Call miniterrno so that we can safely make system calls
	// before calling minit on m0.
	asmcgocall(unsafe.Pointer(abi.FuncPCABI0(miniterrno)), unsafe.Pointer(&libc____errno))

	ncpu = getncpu()
	if physPageSize == 0 {
		physPageSize = getPageSize()
	}
}
>>>>>>> /tmp/go122/src/./runtime/os3_solaris.go

//extern getexecname
func getexecname() *byte

//extern getpagesize
func getPageSize() int32

//extern sysconf
func sysconf(int32) _C_long

func osinit() {
	ncpu = getncpu()
	if physPageSize == 0 {
		physPageSize = uintptr(getPageSize())
	}
}

<<<<<<< go/./runtime/os3_solaris.go
func sysargs(argc int32, argv **byte) {
	executablePath = gostringnocopy(getexecname())
=======
func exitThread(wait *atomic.Uint32) {
	// We should never reach exitThread on Solaris because we let
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

// Called to initialize a new m (including the bootstrap m).
// Called on the parent thread (main thread in case of bootstrap), can allocate memory.
func mpreinit(mp *m) {
	mp.gsignal = malg(32 * 1024)
	mp.gsignal.m = mp
>>>>>>> /tmp/go122/src/./runtime/os3_solaris.go
}

//go:linkname solarisExecutablePath os.solarisExecutablePath

<<<<<<< go/./runtime/os3_solaris.go
// solarisExecutablePath is called from the os package to fetch the
// saved executable path.
func solarisExecutablePath() string {
	return executablePath
=======
// Called to initialize a new m (including the bootstrap m).
// Called on the new thread, cannot allocate memory.
func minit() {
	asmcgocall(unsafe.Pointer(abi.FuncPCABI0(miniterrno)), unsafe.Pointer(&libc____errno))

	minitSignals()

	getg().m.procid = uint64(pthread_self())
}

// Called from dropm to undo the effect of an minit.
func unminit() {
	unminitSignals()
	getg().m.procid = 0
}

// Called from exitm, but not from drop, to undo the effect of thread-owned
// resources in minit, semacreate, or elsewhere. Do not take locks after calling this.
func mdestroy(mp *m) {
}

func sigtramp()

//go:nosplit
//go:nowritebarrierrec
func setsig(i uint32, fn uintptr) {
	var sa sigactiont

	sa.sa_flags = _SA_SIGINFO | _SA_ONSTACK | _SA_RESTART
	sa.sa_mask = sigset_all
	if fn == abi.FuncPCABIInternal(sighandler) { // abi.FuncPCABIInternal(sighandler) matches the callers in signal_unix.go
		fn = abi.FuncPCABI0(sigtramp)
	}
	*((*uintptr)(unsafe.Pointer(&sa._funcptr))) = fn
	sigaction(i, &sa, nil)
}

//go:nosplit
//go:nowritebarrierrec
func setsigstack(i uint32) {
	var sa sigactiont
	sigaction(i, nil, &sa)
	if sa.sa_flags&_SA_ONSTACK != 0 {
		return
	}
	sa.sa_flags |= _SA_ONSTACK
	sigaction(i, &sa, nil)
}

//go:nosplit
//go:nowritebarrierrec
func getsig(i uint32) uintptr {
	var sa sigactiont
	sigaction(i, nil, &sa)
	return *((*uintptr)(unsafe.Pointer(&sa._funcptr)))
}

// setSignalstackSP sets the ss_sp field of a stackt.
//
//go:nosplit
func setSignalstackSP(s *stackt, sp uintptr) {
	*(*uintptr)(unsafe.Pointer(&s.ss_sp)) = sp
}

//go:nosplit
//go:nowritebarrierrec
func sigaddset(mask *sigset, i int) {
	mask.__sigbits[(i-1)/32] |= 1 << ((uint32(i) - 1) & 31)
}

func sigdelset(mask *sigset, i int) {
	mask.__sigbits[(i-1)/32] &^= 1 << ((uint32(i) - 1) & 31)
}

//go:nosplit
func (c *sigctxt) fixsigcode(sig uint32) {
>>>>>>> /tmp/go122/src/./runtime/os3_solaris.go
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
