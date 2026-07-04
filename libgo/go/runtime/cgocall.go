// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Cgo call and callback support.

package runtime

import (
	"internal/goarch"
	"internal/goexperiment"
	"unsafe"
)

// Functions called by cgo-generated code.
//go:linkname cgoCheckPointer
//go:linkname cgoCheckResult

var ncgocall uint64 // number of cgo calls in total for dead m

<<<<<<< go/./runtime/cgocall.go
=======
// Call from Go to C.
//
// This must be nosplit because it's used for syscalls on some
// platforms. Syscalls may have untyped arguments on the stack, so
// it's not safe to grow or scan the stack.
//
//go:nosplit
func cgocall(fn, arg unsafe.Pointer) int32 {
	if !iscgo && GOOS != "solaris" && GOOS != "illumos" && GOOS != "windows" {
		throw("cgocall unavailable")
	}

	if fn == nil {
		throw("cgocall nil")
	}

	if raceenabled {
		racereleasemerge(unsafe.Pointer(&racecgosync))
	}

	mp := getg().m
	mp.ncgocall++

	// Reset traceback.
	mp.cgoCallers[0] = 0

	// Announce we are entering a system call
	// so that the scheduler knows to create another
	// M to run goroutines while we are in the
	// foreign code.
	//
	// The call to asmcgocall is guaranteed not to
	// grow the stack and does not allocate memory,
	// so it is safe to call while "in a system call", outside
	// the $GOMAXPROCS accounting.
	//
	// fn may call back into Go code, in which case we'll exit the
	// "system call", run the Go code (which may grow the stack),
	// and then re-enter the "system call" reusing the PC and SP
	// saved by entersyscall here.
	entersyscall()

	// Tell asynchronous preemption that we're entering external
	// code. We do this after entersyscall because this may block
	// and cause an async preemption to fail, but at this point a
	// sync preemption will succeed (though this is not a matter
	// of correctness).
	osPreemptExtEnter(mp)

	mp.incgo = true
	// We use ncgo as a check during execution tracing for whether there is
	// any C on the call stack, which there will be after this point. If
	// there isn't, we can use frame pointer unwinding to collect call
	// stacks efficiently. This will be the case for the first Go-to-C call
	// on a stack, so it's preferable to update it here, after we emit a
	// trace event in entersyscall above.
	mp.ncgo++

	errno := asmcgocall(fn, arg)

	// Update accounting before exitsyscall because exitsyscall may
	// reschedule us on to a different M.
	mp.incgo = false
	mp.ncgo--

	osPreemptExtExit(mp)

	exitsyscall()

	// Note that raceacquire must be called only after exitsyscall has
	// wired this M to a P.
	if raceenabled {
		raceacquire(unsafe.Pointer(&racecgosync))
	}

	// From the garbage collector's perspective, time can move
	// backwards in the sequence above. If there's a callback into
	// Go code, GC will see this function at the call to
	// asmcgocall. When the Go call later returns to C, the
	// syscall PC/SP is rolled back and the GC sees this function
	// back at the call to entersyscall. Normally, fn and arg
	// would be live at entersyscall and dead at asmcgocall, so if
	// time moved backwards, GC would see these arguments as dead
	// and then live. Prevent these undead arguments from crashing
	// GC by forcing them to stay live across this time warp.
	KeepAlive(fn)
	KeepAlive(arg)
	KeepAlive(mp)

	return errno
}

// Set or reset the system stack bounds for a callback on sp.
//
// Must be nosplit because it is called by needm prior to fully initializing
// the M.
//
//go:nosplit
func callbackUpdateSystemStack(mp *m, sp uintptr, signal bool) {
	g0 := mp.g0
	if sp > g0.stack.lo && sp <= g0.stack.hi {
		// Stack already in bounds, nothing to do.
		return
	}

	if mp.ncgo > 0 {
		// ncgo > 0 indicates that this M was in Go further up the stack
		// (it called C and is now receiving a callback). It is not
		// safe for the C call to change the stack out from under us.

		// Note that this case isn't possible for signal == true, as
		// that is always passing a new M from needm.

		// Stack is bogus, but reset the bounds anyway so we can print.
		hi := g0.stack.hi
		lo := g0.stack.lo
		g0.stack.hi = sp + 1024
		g0.stack.lo = sp - 32*1024
		g0.stackguard0 = g0.stack.lo + stackGuard
		g0.stackguard1 = g0.stackguard0

		print("M ", mp.id, " procid ", mp.procid, " runtime: cgocallback with sp=", hex(sp), " out of bounds [", hex(lo), ", ", hex(hi), "]")
		print("\n")
		exit(2)
	}

	// This M does not have Go further up the stack. However, it may have
	// previously called into Go, initializing the stack bounds. Between
	// that call returning and now the stack may have changed (perhaps the
	// C thread is running a coroutine library). We need to update the
	// stack bounds for this case.
	//
	// Set the stack bounds to match the current stack. If we don't
	// actually know how big the stack is, like we don't know how big any
	// scheduling stack is, but we assume there's at least 32 kB. If we
	// can get a more accurate stack bound from pthread, use that, provided
	// it actually contains SP..
	g0.stack.hi = sp + 1024
	g0.stack.lo = sp - 32*1024
	if !signal && _cgo_getstackbound != nil {
		// Don't adjust if called from the signal handler.
		// We are on the signal stack, not the pthread stack.
		// (We could get the stack bounds from sigaltstack, but
		// we're getting out of the signal handler very soon
		// anyway. Not worth it.)
		var bounds [2]uintptr
		asmcgocall(_cgo_getstackbound, unsafe.Pointer(&bounds))
		// getstackbound is an unsupported no-op on Windows.
		//
		// Don't use these bounds if they don't contain SP. Perhaps we
		// were called by something not using the standard thread
		// stack.
		if bounds[0] != 0 && sp > bounds[0] && sp <= bounds[1] {
			g0.stack.lo = bounds[0]
			g0.stack.hi = bounds[1]
		}
	}
	g0.stackguard0 = g0.stack.lo + stackGuard
	g0.stackguard1 = g0.stackguard0
}

// Call from C back to Go. fn must point to an ABIInternal Go entry-point.
//
//go:nosplit
func cgocallbackg(fn, frame unsafe.Pointer, ctxt uintptr) {
	gp := getg()
	if gp != gp.m.curg {
		println("runtime: bad g in cgocallback")
		exit(2)
	}

	sp := gp.m.g0.sched.sp // system sp saved by cgocallback.
	callbackUpdateSystemStack(gp.m, sp, false)

	// The call from C is on gp.m's g0 stack, so we must ensure
	// that we stay on that M. We have to do this before calling
	// exitsyscall, since it would otherwise be free to move us to
	// a different M. The call to unlockOSThread is in this function
	// after cgocallbackg1, or in the case of panicking, in unwindm.
	lockOSThread()

	checkm := gp.m

	// Save current syscall parameters, so m.syscall can be
	// used again if callback decide to make syscall.
	syscall := gp.m.syscall

	// entersyscall saves the caller's SP to allow the GC to trace the Go
	// stack. However, since we're returning to an earlier stack frame and
	// need to pair with the entersyscall() call made by cgocall, we must
	// save syscall* and let reentersyscall restore them.
	savedsp := unsafe.Pointer(gp.syscallsp)
	savedpc := gp.syscallpc
	exitsyscall() // coming out of cgo call
	gp.m.incgo = false
	if gp.m.isextra {
		gp.m.isExtraInC = false
	}

	osPreemptExtExit(gp.m)

	if gp.nocgocallback {
		panic("runtime: function marked with #cgo nocallback called back into Go")
	}

	cgocallbackg1(fn, frame, ctxt)

	// At this point we're about to call unlockOSThread.
	// The following code must not change to a different m.
	// This is enforced by checking incgo in the schedule function.
	gp.m.incgo = true
	unlockOSThread()

	if gp.m.isextra {
		gp.m.isExtraInC = true
	}

	if gp.m != checkm {
		throw("m changed unexpectedly in cgocallbackg")
	}

	osPreemptExtEnter(gp.m)

	// going back to cgo call
	reentersyscall(savedpc, uintptr(savedsp))

	gp.m.syscall = syscall
}

func cgocallbackg1(fn, frame unsafe.Pointer, ctxt uintptr) {
	gp := getg()

	if gp.m.needextram || extraMWaiters.Load() > 0 {
		gp.m.needextram = false
		systemstack(newextram)
	}

	if ctxt != 0 {
		s := append(gp.cgoCtxt, ctxt)

		// Now we need to set gp.cgoCtxt = s, but we could get
		// a SIGPROF signal while manipulating the slice, and
		// the SIGPROF handler could pick up gp.cgoCtxt while
		// tracing up the stack.  We need to ensure that the
		// handler always sees a valid slice, so set the
		// values in an order such that it always does.
		p := (*slice)(unsafe.Pointer(&gp.cgoCtxt))
		atomicstorep(unsafe.Pointer(&p.array), unsafe.Pointer(&s[0]))
		p.cap = cap(s)
		p.len = len(s)

		defer func(gp *g) {
			// Decrease the length of the slice by one, safely.
			p := (*slice)(unsafe.Pointer(&gp.cgoCtxt))
			p.len--
		}(gp)
	}

	if gp.m.ncgo == 0 {
		// The C call to Go came from a thread not currently running
		// any Go. In the case of -buildmode=c-archive or c-shared,
		// this call may be coming in before package initialization
		// is complete. Wait until it is.
		<-main_init_done
	}

	// Check whether the profiler needs to be turned on or off; this route to
	// run Go code does not use runtime.execute, so bypasses the check there.
	hz := sched.profilehz
	if gp.m.profilehz != hz {
		setThreadCPUProfiler(hz)
	}

	// Add entry to defer stack in case of panic.
	restore := true
	defer unwindm(&restore)

	if raceenabled {
		raceacquire(unsafe.Pointer(&racecgosync))
	}

	// Invoke callback. This function is generated by cmd/cgo and
	// will unpack the argument frame and call the Go function.
	var cb func(frame unsafe.Pointer)
	cbFV := funcval{uintptr(fn)}
	*(*unsafe.Pointer)(unsafe.Pointer(&cb)) = noescape(unsafe.Pointer(&cbFV))
	cb(frame)

	if raceenabled {
		racereleasemerge(unsafe.Pointer(&racecgosync))
	}

	// Do not unwind m->g0->sched.sp.
	// Our caller, cgocallback, will do that.
	restore = false
}

func unwindm(restore *bool) {
	if *restore {
		// Restore sp saved by cgocallback during
		// unwind of g's stack (see comment at top of file).
		mp := acquirem()
		sched := &mp.g0.sched
		sched.sp = *(*uintptr)(unsafe.Pointer(sched.sp + alignUp(sys.MinFrameSize, sys.StackAlign)))

		// Do the accounting that cgocall will not have a chance to do
		// during an unwind.
		//
		// In the case where a Go call originates from C, ncgo is 0
		// and there is no matching cgocall to end.
		if mp.ncgo > 0 {
			mp.incgo = false
			mp.ncgo--
			osPreemptExtExit(mp)
		}

		// Undo the call to lockOSThread in cgocallbackg, only on the
		// panicking path. In normal return case cgocallbackg will call
		// unlockOSThread, ensuring no preemption point after the unlock.
		// Here we don't need to worry about preemption, because we're
		// panicking out of the callback and unwinding the g0 stack,
		// instead of reentering cgo (which requires the same thread).
		unlockOSThread()

		releasem(mp)
	}
}

// called from assembly.
func badcgocallback() {
	throw("misaligned stack in cgocallback")
}

// called from (incomplete) assembly.
func cgounimpl() {
	throw("cgo not implemented")
}

var racecgosync uint64 // represents possible synchronization in C code

>>>>>>> /tmp/go122/src/./runtime/cgocall.go
// Pointer checking for cgo code.

// We want to detect all cases where a program that does not use
// unsafe makes a cgo call passing a Go pointer to memory that
// contains an unpinned Go pointer. Here a Go pointer is defined as a
// pointer to memory allocated by the Go runtime. Programs that use
// unsafe can evade this restriction easily, so we don't try to catch
// them. The cgo program will rewrite all possibly bad pointer
// arguments to call cgoCheckPointer, where we can catch cases of a Go
// pointer pointing to an unpinned Go pointer.

// Complicating matters, taking the address of a slice or array
// element permits the C program to access all elements of the slice
// or array. In that case we will see a pointer to a single element,
// but we need to check the entire data structure.

// The cgoCheckPointer call takes additional arguments indicating that
// it was called on an address expression. An additional argument of
// true means that it only needs to check a single element. An
// additional argument of a slice or array means that it needs to
// check the entire slice/array, but nothing else. Otherwise, the
// pointer could be anything, and we check the entire heap object,
// which is conservative but safe.

// When and if we implement a moving garbage collector,
// cgoCheckPointer will pin the pointer for the duration of the cgo
// call.  (This is necessary but not sufficient; the cgo program will
// also have to change to pin Go pointers that cannot point to Go
// pointers.)

// cgoCheckPointer checks if the argument contains a Go pointer that
// points to an unpinned Go pointer, and panics if it does.
func cgoCheckPointer(ptr any, arg any) {
	if !goexperiment.CgoCheck2 && debug.cgocheck == 0 {
		return
	}

	ep := efaceOf(&ptr)
	t := ep._type

	top := true
	if arg != nil && (t.kind&kindMask == kindPtr || t.kind&kindMask == kindUnsafePointer) {
		p := ep.data
		if t.kind&kindDirectIface == 0 {
			p = *(*unsafe.Pointer)(p)
		}
		if p == nil || !cgoIsGoPointer(p) {
			return
		}
		aep := efaceOf(&arg)
		switch aep._type.kind & kindMask {
		case kindBool:
			if t.kind&kindMask == kindUnsafePointer {
				// We don't know the type of the element.
				break
			}
			pt := (*ptrtype)(unsafe.Pointer(t))
			cgoCheckArg(pt.elem, p, true, false, cgoCheckPointerFail)
			return
		case kindSlice:
			// Check the slice rather than the pointer.
			ep = aep
			t = ep._type
		case kindArray:
			// Check the array rather than the pointer.
			// Pass top as false since we have a pointer
			// to the array.
			ep = aep
			t = ep._type
			top = false
		default:
			throw("can't happen")
		}
	}

	cgoCheckArg(t, ep.data, t.kind&kindDirectIface == 0, top, cgoCheckPointerFail)
}

const cgoCheckPointerFail = "cgo argument has Go pointer to unpinned Go pointer"
const cgoResultFail = "cgo result is unpinned Go pointer or points to unpinned Go pointer"

// cgoCheckArg is the real work of cgoCheckPointer. The argument p
// is either a pointer to the value (of type t), or the value itself,
// depending on indir. The top parameter is whether we are at the top
// level, where Go pointers are allowed. Go pointers to pinned objects are
// allowed as long as they don't reference other unpinned pointers.
func cgoCheckArg(t *_type, p unsafe.Pointer, indir, top bool, msg string) {
	if t.ptrdata == 0 || p == nil {
		// If the type has no pointers there is nothing to do.
		return
	}

	switch t.kind & kindMask {
	default:
		throw("can't happen")
	case kindArray:
		at := (*arraytype)(unsafe.Pointer(t))
		if !indir {
			if at.len != 1 {
				throw("can't happen")
			}
			cgoCheckArg(at.elem, p, at.elem.kind&kindDirectIface == 0, top, msg)
			return
		}
		for i := uintptr(0); i < at.len; i++ {
			cgoCheckArg(at.elem, p, true, top, msg)
			p = add(p, at.elem.size)
		}
	case kindChan, kindMap:
		// These types contain internal pointers that will
		// always be allocated in the Go heap. It's never OK
		// to pass them to C.
		panic(errorString(msg))
	case kindFunc:
		if indir {
			p = *(*unsafe.Pointer)(p)
		}
		if !cgoIsGoPointer(p) {
			return
		}
		panic(errorString(msg))
	case kindInterface:
		it := *(**_type)(p)
		if it == nil {
			return
		}
		// A type known at compile time is OK since it's
		// constant. A type not known at compile time will be
		// in the heap and will not be OK.
		if inheap(uintptr(unsafe.Pointer(it))) {
			panic(errorString(msg))
		}
		p = *(*unsafe.Pointer)(add(p, goarch.PtrSize))
		if !cgoIsGoPointer(p) {
			return
		}
		if !top && !isPinned(p) {
			panic(errorString(msg))
		}
		cgoCheckArg(it, p, it.kind&kindDirectIface == 0, false, msg)
	case kindSlice:
		st := (*slicetype)(unsafe.Pointer(t))
		s := (*slice)(p)
		p = s.array
		if p == nil || !cgoIsGoPointer(p) {
			return
		}
		if !top && !isPinned(p) {
			panic(errorString(msg))
		}
		if st.elem.ptrdata == 0 {
			return
		}
		for i := 0; i < s.cap; i++ {
			cgoCheckArg(st.elem, p, true, false, msg)
			p = add(p, st.elem.size)
		}
	case kindString:
		ss := (*stringStruct)(p)
		if !cgoIsGoPointer(ss.str) {
			return
		}
		if !top && !isPinned(ss.str) {
			panic(errorString(msg))
		}
	case kindStruct:
		st := (*structtype)(unsafe.Pointer(t))
		if !indir {
			if len(st.fields) != 1 {
				throw("can't happen")
			}
			cgoCheckArg(st.fields[0].typ, p, st.fields[0].typ.kind&kindDirectIface == 0, top, msg)
			return
		}
		for _, f := range st.fields {
			if f.typ.ptrdata == 0 {
				continue
			}
			cgoCheckArg(f.typ, add(p, f.offset()), true, top, msg)
		}
	case kindPtr, kindUnsafePointer:
		if indir {
			p = *(*unsafe.Pointer)(p)
			if p == nil {
				return
			}
		}

		if !cgoIsGoPointer(p) {
			return
		}
		if !top && !isPinned(p) {
			panic(errorString(msg))
		}

		cgoCheckUnknownPointer(p, msg)
	}
}

// cgoCheckUnknownPointer is called for an arbitrary pointer into Go
// memory. It checks whether that Go memory contains any other
// pointer into unpinned Go memory. If it does, we panic.
// The return values are unused but useful to see in panic tracebacks.
func cgoCheckUnknownPointer(p unsafe.Pointer, msg string) (base, i uintptr) {
	if inheap(uintptr(p)) {
		b, span, _ := findObject(uintptr(p), 0, 0, false)
		base = b
		if base == 0 {
			return
		}
<<<<<<< go/./runtime/cgocall.go
		hbits := heapBitsForAddr(base)
		n := span.elemsize
		for i = uintptr(0); i < n; i += goarch.PtrSize {
			if !hbits.morePointers() {
				// No more possible pointers.
				break
=======
		if goexperiment.AllocHeaders {
			tp := span.typePointersOfUnchecked(base)
			for {
				var addr uintptr
				if tp, addr = tp.next(base + span.elemsize); addr == 0 {
					break
				}
				pp := *(*unsafe.Pointer)(unsafe.Pointer(addr))
				if cgoIsGoPointer(pp) && !isPinned(pp) {
					panic(errorString(msg))
				}
>>>>>>> /tmp/go122/src/./runtime/cgocall.go
			}
<<<<<<< go/./runtime/cgocall.go
			if hbits.isPointer() && cgoIsGoPointer(*(*unsafe.Pointer)(unsafe.Pointer(base + i))) {
				panic(errorString(msg))
=======
		} else {
			n := span.elemsize
			hbits := heapBitsForAddr(base, n)
			for {
				var addr uintptr
				if hbits, addr = hbits.next(); addr == 0 {
					break
				}
				pp := *(*unsafe.Pointer)(unsafe.Pointer(addr))
				if cgoIsGoPointer(pp) && !isPinned(pp) {
					panic(errorString(msg))
				}
>>>>>>> /tmp/go122/src/./runtime/cgocall.go
			}
			hbits = hbits.next()
		}
		return
	}

	lo := 0
	hi := len(gcRootsIndex)
	for lo < hi {
		m := lo + (hi-lo)/2
		pr := gcRootsIndex[m]
		addr := uintptr(pr.decl)
		if cgoInRange(p, addr, addr+pr.size) {
			cgoCheckBits(pr.decl, pr.gcdata, 0, pr.ptrdata)
			return
		}
		if uintptr(p) < addr {
			hi = m
		} else {
			lo = m + 1
		}
	}

	return
}

// cgoIsGoPointer reports whether the pointer is a Go pointer--a
// pointer to Go memory. We only care about Go memory that might
// contain pointers.
//
//go:nosplit
//go:nowritebarrierrec
func cgoIsGoPointer(p unsafe.Pointer) bool {
	if p == nil {
		return false
	}

	if inHeapOrStack(uintptr(p)) {
		return true
	}

	roots := gcRoots
	for roots != nil {
		for i := 0; i < roots.count; i++ {
			pr := roots.roots[i]
			addr := uintptr(pr.decl)
			if cgoInRange(p, addr, addr+pr.size) {
				return true
			}
		}
		roots = roots.next
	}

	return false
}

// cgoInRange reports whether p is between start and end.
//
//go:nosplit
//go:nowritebarrierrec
func cgoInRange(p unsafe.Pointer, start, end uintptr) bool {
	return start <= uintptr(p) && uintptr(p) < end
}

// cgoCheckResult is called to check the result parameter of an
// exported Go function. It panics if the result is or contains any
// other pointer into unpinned Go memory.
func cgoCheckResult(val any) {
	if !goexperiment.CgoCheck2 && debug.cgocheck == 0 {
		return
	}

	ep := efaceOf(&val)
	t := ep._type
	cgoCheckArg(t, ep.data, t.kind&kindDirectIface == 0, false, cgoResultFail)
}
