// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/runtime/atomic"
	"unsafe"
)

//go:generate go run wincallback.go
//go:generate go run mkduff.go
//go:generate go run mkfastlog2table.go
//go:generate go run mklockrank.go -o lockrank.go

// For gccgo, while we still have C runtime code, use go:linkname to
// export some functions.
//
//go:linkname tickspersecond

var ticksLock mutex
var ticksVal uint64

// Note: Called by runtime/pprof in addition to runtime code.
func tickspersecond() int64 {
	r := int64(atomic.Load64(&ticksVal))
	if r != 0 {
		return r
	}
	lock(&ticksLock)
	r = int64(ticksVal)
	if r == 0 {
		t0 := nanotime()
		c0 := cputicks()
		usleep(100 * 1000)
		t1 := nanotime()
		c1 := cputicks()
		if t1 == t0 {
			t1++
		}
		r = (c1 - c0) * 1000 * 1000 * 1000 / (t1 - t0)
		if r == 0 {
			r++
		}
		atomic.Store64(&ticksVal, uint64(r))
	}
	unlock(&ticksLock)
	return r
}

var envs []string
var argslice []string

//go:linkname syscall_runtime_envs syscall.runtime__envs
func syscall_runtime_envs() []string { return append([]string{}, envs...) }

//go:linkname syscall_Getpagesize syscall.Getpagesize
func syscall_Getpagesize() int { return int(physPageSize) }

//go:linkname os_runtime_args os.runtime__args
func os_runtime_args() []string { return append([]string{}, argslice...) }

//go:linkname syscall_Exit syscall.Exit
//go:nosplit
func syscall_Exit(code int) {
	exit(int32(code))
}

// Temporary, for the gccgo runtime code written in C.
//go:linkname get_envs runtime_get_envs
func get_envs() []string { return envs }

//go:linkname get_args runtime_get_args
func get_args() []string { return argslice }

var godebugDefault string
var godebugUpdate atomic.Pointer[func(string, string)]
var godebugEnv atomic.Pointer[string] // set by parsedebugvars
var godebugNewIncNonDefault atomic.Pointer[func(string) func()]

//go:linkname godebug_setUpdate internal_1godebug.setUpdate
func godebug_setUpdate(update func(string, string)) {
	p := new(func(string, string))
	*p = update
	godebugUpdate.Store(p)
	godebugNotify(false)
}

//go:linkname godebug_setNewIncNonDefault internal_1godebug.setNewIncNonDefault
func godebug_setNewIncNonDefault(newIncNonDefault func(string) func()) {
	p := new(func(string) func())
	*p = newIncNonDefault
	godebugNewIncNonDefault.Store(p)
}

// A godebugInc provides access to internal/godebug's IncNonDefault function
// for a given GODEBUG setting.
// Calls before internal/godebug registers itself are dropped on the floor.
type godebugInc struct {
	name string
	inc  atomic.Pointer[func()]
}

func (g *godebugInc) IncNonDefault() {
	inc := g.inc.Load()
	if inc == nil {
		newInc := godebugNewIncNonDefault.Load()
		if newInc == nil {
			return
		}
		inc = new(func())
		*inc = (*newInc)(g.name)
		if raceenabled {
			racereleasemerge(unsafe.Pointer(&g.inc))
		}
		if !g.inc.CompareAndSwap(nil, inc) {
			inc = g.inc.Load()
		}
	}
	if raceenabled {
		raceacquire(unsafe.Pointer(&g.inc))
	}
	(*inc)()
}

func godebugNotify(envChanged bool) {
	update := godebugUpdate.Load()
	var env string
	if p := godebugEnv.Load(); p != nil {
		env = *p
	}
	if envChanged {
		reparsedebugvars(env)
	}
	if update != nil {
		(*update)(godebugDefault, env)
	}
}

//go:linkname syscall_runtimeSetenv syscall.runtimeSetenv
func syscall_runtimeSetenv(key, value string) {
	setenv_c(key, value)
	if key == "GODEBUG" {
		p := new(string)
		*p = value
		godebugEnv.Store(p)
		godebugNotify(true)
	}
}

//go:linkname syscall_runtimeUnsetenv syscall.runtimeUnsetenv
func syscall_runtimeUnsetenv(key string) {
	unsetenv_c(key)
	if key == "GODEBUG" {
		godebugEnv.Store(nil)
		godebugNotify(true)
	}
}

// writeErrStr writes a string to descriptor 2.
// If SetCrashOutput(f) was called, it also writes to f.
//
//go:nosplit
func writeErrStr(s string) {
	writeErrData(unsafe.StringData(s), int32(len(s)))
}

// writeErrData is the common parts of writeErr{,Str}.
//
//go:nosplit
func writeErrData(data *byte, n int32) {
	write(2, unsafe.Pointer(data), n)

	// If crashing, print a copy to the SetCrashOutput fd.
	gp := getg()
	if gp != nil && gp.m.dying > 0 ||
		gp == nil && panicking.Load() > 0 {
		if fd := crashFD.Load(); fd != ^uintptr(0) {
			write(fd, unsafe.Pointer(data), n)
		}
	}
}

// crashFD is an optional file descriptor to use for fatal panics, as
// set by debug.SetCrashOutput (see #42888). If it is a valid fd (not
// all ones), writeErr and related functions write to it in addition
// to standard error.
//
// Initialized to -1 in schedinit.
var crashFD atomic.Uintptr

//go:linkname setCrashFD
func setCrashFD(fd uintptr) uintptr {
	// Don't change the crash FD if a crash is already in progress.
	//
	// Unlike the case below, this is not required for correctness, but it
	// is generally nicer to have all of the crash output go to the same
	// place rather than getting split across two different FDs.
	if panicking.Load() > 0 {
		return ^uintptr(0)
	}

	old := crashFD.Swap(fd)

	// If we are panicking, don't return the old FD to runtime/debug for
	// closing. writeErrData may have already read the old FD from crashFD
	// before the swap and closing it would cause the write to be lost [1].
	// The old FD will never be closed, but we are about to crash anyway.
	//
	// On the writeErrData thread, panicking.Add(1) happens-before
	// crashFD.Load() [2].
	//
	// On this thread, swapping old FD for new in crashFD happens-before
	// panicking.Load() > 0.
	//
	// Therefore, if panicking.Load() == 0 here (old FD will be closed), it
	// is impossible for the writeErrData thread to observe
	// crashFD.Load() == old FD.
	//
	// [1] Or, if really unlucky, another concurrent open could reuse the
	// FD, sending the write into an unrelated file.
	//
	// [2] If gp != nil, it occurs when incrementing gp.m.dying in
	// startpanic_m. If gp == nil, we read panicking.Load() > 0, so an Add
	// must have happened-before.
	if panicking.Load() > 0 {
		return ^uintptr(0)
	}
	return old
}

// auxv is populated on relevant platforms but defined here for all platforms
// so x/sys/cpu can assume the getAuxv symbol exists without keeping its list
// of auxv-using GOOS build tags in sync.
//
// It contains an even number of elements, (tag, value) pairs.
var auxv []uintptr

// The go:linkname is needed so that gccgo emits getAuxv under the
// runtime.getAuxv symbol name pulled by x/sys/cpu via go:linkname.
//
//go:linkname getAuxv runtime.getAuxv
func getAuxv() []uintptr { return auxv } // accessed from x/sys/cpu; see issue 57336
