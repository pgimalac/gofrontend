// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/chacha8rand"
	"internal/goarch"
	"internal/runtime/atomic"
	"unsafe"
)

// defined constants
const (
	// G status
	//
	// Beyond indicating the general state of a G, the G status
	// acts like a lock on the goroutine's stack (and hence its
	// ability to execute user code).
	//
	// If you add to this list, add to the list
	// of "okay during garbage collection" status
	// in mgcmark.go too.
	//
	// TODO(austin): The _Gscan bit could be much lighter-weight.
	// For example, we could choose not to run _Gscanrunnable
	// goroutines found in the run queue, rather than CAS-looping
	// until they become _Grunnable. And transitions like
	// _Gscanwaiting -> _Gscanrunnable are actually okay because
	// they don't affect stack ownership.

	// _Gidle means this goroutine was just allocated and has not
	// yet been initialized.
	_Gidle = iota // 0

	// _Grunnable means this goroutine is on a run queue. It is
	// not currently executing user code. The stack is not owned.
	_Grunnable // 1

	// _Grunning means this goroutine may execute user code. The
	// stack is owned by this goroutine. It is not on a run queue.
	// It is assigned an M (g.m is valid) and it usually has a P
	// (g.m.p is valid), but there are small windows of time where
	// it might not, namely upon entering and exiting _Gsyscall.
	_Grunning // 2

	// _Gsyscall means this goroutine is executing a system call.
	// It is not executing user code. The stack is owned by this
	// goroutine. It is not on a run queue. It is assigned an M.
	// It may have a P attached, but it does not own it. Code
	// executing in this state must not touch g.m.p.
	_Gsyscall // 3

	// _Gwaiting means this goroutine is blocked in the runtime.
	// It is not executing user code. It is not on a run queue,
	// but should be recorded somewhere (e.g., a channel wait
	// queue) so it can be ready()d when necessary. The stack is
	// not owned *except* that a channel operation may read or
	// write parts of the stack under the appropriate channel
	// lock. Otherwise, it is not safe to access the stack after a
	// goroutine enters _Gwaiting (e.g., it may get moved).
	_Gwaiting // 4

	// _Gmoribund_unused is currently unused, but hardcoded in gdb
	// scripts.
	_Gmoribund_unused // 5

	// _Gdead means this goroutine is currently unused. It may be
	// just exited, on a free list, or just being initialized. It
	// is not executing user code. It may or may not have a stack
	// allocated. The G and its stack (if any) are owned by the M
	// that is exiting the G or that obtained the G from the free
	// list.
	_Gdead // 6

	// _Genqueue_unused is currently unused.
	_Genqueue_unused // 7

	// _Gcopystack means this goroutine's stack is being moved. It
	// is not executing user code and is not on a run queue. The
	// stack is owned by the goroutine that put it in _Gcopystack.
	_Gcopystack // 8

	// _Gpreempted means this goroutine stopped itself for a
	// suspendG preemption. It is like _Gwaiting, but nothing is
	// yet responsible for ready()ing it. Some suspendG must CAS
	// the status to _Gwaiting to take responsibility for
	// ready()ing this G.
	_Gpreempted // 9

	// _Gexitingsyscall means this goroutine is exiting from a
	// system call. This is like _Gsyscall, but the GC should not
	// scan its stack. Currently this is only used in exitsyscall0
	// as a transient state when it drops the G.
	_Gexitingsyscall // 10

	// _Gscan combined with one of the above states other than
	// _Grunning indicates that GC is scanning the stack. The
	// goroutine is not executing user code and the stack is owned
	// by the goroutine that set the _Gscan bit.
	//
	// _Gscanrunning is different: it is used to briefly block
	// state transitions while GC signals the G to scan its own
	// stack. This is otherwise like _Grunning.
	//
	// atomicstatus&~Gscan gives the state the goroutine will
	// return to when the scan completes.
	_Gscan          = 0x1000
	_Gscanrunnable  = _Gscan + _Grunnable  // 0x1001
	_Gscanrunning   = _Gscan + _Grunning   // 0x1002
	_Gscansyscall   = _Gscan + _Gsyscall   // 0x1003
	_Gscanwaiting   = _Gscan + _Gwaiting   // 0x1004
	_Gscanpreempted = _Gscan + _Gpreempted // 0x1009
	_Gscanleaked    = _Gscan + _Gleaked    // 0x100a
	_Gscandeadextra = _Gscan + _Gdeadextra // 0x100b
)

const (
	// P status

	// _Pidle means a P is not being used to run user code or the
	// scheduler. Typically, it's on the idle P list and available
	// to the scheduler, but it may just be transitioning between
	// other states.
	//
	// The P is owned by the idle list or by whatever is
	// transitioning its state. Its run queue is empty.
	_Pidle = iota

	// _Prunning means a P is owned by an M and is being used to
	// run user code or the scheduler. Only the M that owns this P
	// is allowed to change the P's status from _Prunning. The M
	// may transition the P to _Pidle (if it has no more work to
	// do), or _Pgcstop (to halt for the GC). The M may also hand
	// ownership of the P off directly to another M (for example,
	// to schedule a locked G).
	_Prunning

	// _Psyscall_unused is a now-defunct state for a P. A P is
	// identified as "in a system call" by looking at the goroutine's
	// state.
	_Psyscall_unused

	// _Pgcstop means a P is halted for STW and owned by the M
	// that stopped the world. The M that stopped the world
	// continues to use its P, even in _Pgcstop. Transitioning
	// from _Prunning to _Pgcstop causes an M to release its P and
	// park.
	//
	// The P retains its run queue and startTheWorld will restart
	// the scheduler on Ps with non-empty run queues.
	_Pgcstop

	// _Pdead means a P is no longer used (GOMAXPROCS shrank). We
	// reuse Ps if GOMAXPROCS increases. A dead P is mostly
	// stripped of its resources, though a few things remain
	// (e.g., trace buffers).
	_Pdead
)

// Mutual exclusion locks.  In the uncontended case,
// as fast as spin locks (just a few user-level instructions),
// but on the contention path they sleep in the kernel.
// A zeroed Mutex is unlocked (no need to initialize each lock).
// Initialization is helpful for static lock ranking, but not required.
type mutex struct {
	// Empty struct if lock ranking is disabled, otherwise includes the lock rank
	lockRankStruct
	// Futex-based impl treats it as uint32 key,
	// while sema-based impl as M* waitm.
	// Used to be a union, but unions break precise GC.
	key uintptr
}

type funcval struct {
	fn uintptr
	// variable-size, fn-specific data here
}

// The representation of a non-empty interface.
// See comment in iface.go for more details on this struct.
type iface struct {
	tab  unsafe.Pointer
	data unsafe.Pointer
}

// The representation of an empty interface.
// See comment in iface.go for more details on this struct.
type eface struct {
	_type *_type
	data  unsafe.Pointer
}

func efaceOf(ep *any) *eface {
	return (*eface)(unsafe.Pointer(ep))
}

// The guintptr, muintptr, and puintptr are all used to bypass write barriers.
// It is particularly important to avoid write barriers when the current P has
// been released, because the GC thinks the world is stopped, and an
// unexpected write barrier would not be synchronized with the GC,
// which can lead to a half-executed write barrier that has marked the object
// but not queued it. If the GC skips the object and completes before the
// queuing can occur, it will incorrectly free the object.
//
// We tried using special assignment functions invoked only when not
// holding a running P, but then some updates to a particular memory
// word went through write barriers and some did not. This breaks the
// write barrier shadow checking mode, and it is also scary: better to have
// a word that is completely ignored by the GC than to have one for which
// only a few updates are ignored.
//
// Gs and Ps are always reachable via true pointers in the
// allgs and allp lists or (during allocation before they reach those lists)
// from stack variables.
//
// Ms are always reachable via true pointers either from allm or
// freem. Unlike Gs and Ps we do free Ms, so it's important that
// nothing ever hold an muintptr across a safe point.

// A guintptr holds a goroutine pointer, but typed as a uintptr
// to bypass write barriers. It is used in the Gobuf goroutine state
// and in scheduling lists that are manipulated without a P.
//
// The Gobuf.g goroutine pointer is almost always updated by assembly code.
// In one of the few places it is updated by Go code - func save - it must be
// treated as a uintptr to avoid a write barrier being emitted at a bad time.
// Instead of figuring out how to emit the write barriers missing in the
// assembly manipulation, we change the type of the field to uintptr,
// so that it does not require write barriers at all.
//
// Goroutine structs are published in the allg list and never freed.
// That will keep the goroutine structs from being collected.
// There is never a time that Gobuf.g's contain the only references
// to a goroutine: the publishing of the goroutine in allg comes first.
// Goroutine pointers are also kept in non-GC-visible places like TLS,
// so I can't see them ever moving. If we did want to start moving data
// in the GC, we'd need to allocate the goroutine structs from an
// alternate arena. Using guintptr doesn't make that problem any worse.
// Note that pollDesc.rg, pollDesc.wg also store g in uintptr form,
// so they would need to be updated too if g's start moving.
type guintptr uintptr

//go:nosplit
func (gp guintptr) ptr() *g { return (*g)(unsafe.Pointer(gp)) }

//go:nosplit
func (gp *guintptr) set(g *g) { *gp = guintptr(unsafe.Pointer(g)) }

//go:nosplit
func (gp *guintptr) cas(old, new guintptr) bool {
	return atomic.Casuintptr((*uintptr)(unsafe.Pointer(gp)), uintptr(old), uintptr(new))
}

//go:nosplit
func (gp *g) guintptr() guintptr {
	return guintptr(unsafe.Pointer(gp))
}

// setGNoWB performs *gp = new without a write barrier.
// For times when it's impractical to use a guintptr.
//
//go:nosplit
//go:nowritebarrier
func setGNoWB(gp **g, new *g) {
	(*guintptr)(unsafe.Pointer(gp)).set(new)
}

type puintptr uintptr

//go:nosplit
func (pp puintptr) ptr() *p { return (*p)(unsafe.Pointer(pp)) }

//go:nosplit
func (pp *puintptr) set(p *p) { *pp = puintptr(unsafe.Pointer(p)) }

// muintptr is a *m that is not tracked by the garbage collector.
//
// Because we do free Ms, there are some additional constrains on
// muintptrs:
//
//  1. Never hold an muintptr locally across a safe point.
//
//  2. Any muintptr in the heap must be owned by the M itself so it can
//     ensure it is not in use when the last true *m is released.
type muintptr uintptr

//go:nosplit
func (mp muintptr) ptr() *m { return (*m)(unsafe.Pointer(mp)) }

//go:nosplit
func (mp *muintptr) set(m *m) { *mp = muintptr(unsafe.Pointer(m)) }

// setMNoWB performs *mp = new without a write barrier.
// For times when it's impractical to use an muintptr.
//
//go:nosplit
//go:nowritebarrier
func setMNoWB(mp **m, new *m) {
	(*muintptr)(unsafe.Pointer(mp)).set(new)
}

// sudog represents a g in a wait list, such as for sending/receiving
// on a channel.
//
// sudog is necessary because the g ↔ synchronization object relation
// is many-to-many. A g can be on many wait lists, so there may be
// many sudogs for one g; and many gs may be waiting on the same
// synchronization object, so there may be many sudogs for one object.
//
// sudogs are allocated from a special pool. Use acquireSudog and
// releaseSudog to allocate and free them.
type sudog struct {
	// The following fields are protected by the hchan.lock of the
	// channel this sudog is blocking on. shrinkstack depends on
	// this for sudogs involved in channel ops.

	g *g

	next *sudog
	prev *sudog

	elem maybeTraceablePtr // data element (may point to stack)

	// The following fields are never accessed concurrently.
	// For channels, waitlink is only accessed by g.
	// For semaphores, all fields (including the ones above)
	// are only accessed when holding a semaRoot lock.

	acquiretime int64
	releasetime int64
	ticket      uint32

	// isSelect indicates g is participating in a select, so
	// g.selectDone must be CAS'd to win the wake-up race.
	isSelect bool

	// success indicates whether communication over channel c
	// succeeded. It is true if the goroutine was awoken because a
	// value was delivered over channel c, and false if awoken
	// because c was closed.
	success bool

	// waiters is a count of semaRoot waiting list other than head of list,
	// clamped to a uint16 to fit in unused space.
	// Only meaningful at the head of the list.
	// (If we wanted to be overly clever, we could store a high 16 bits
	// in the second entry in the list.)
	waiters uint16

	parent   *sudog             // semaRoot binary tree
	waitlink *sudog             // g.waiting list or semaRoot
	waittail *sudog             // semaRoot
	c        maybeTraceableChan // channel
}

/*
Not used by gccgo.

type libcall struct {
	fn   uintptr
	n    uintptr // number of parameters
	args uintptr // parameters
	r1   uintptr // return values
	r2   uintptr
	err  uintptr // error number
}

// Stack describes a Go execution stack.
// The bounds of the stack are exactly [lo, hi),
// with no implicit data structures on either side.
type stack struct {
	lo uintptr
	hi uintptr
}
*/

// heldLockInfo gives info on a held lock and the rank of that lock
type heldLockInfo struct {
	lockAddr uintptr
	rank     lockRank
}

type g struct {
	// Stack parameters.
	// stack describes the actual stack memory: [stack.lo, stack.hi).
	// stackguard0 is the stack pointer compared in the Go stack growth prologue.
	// It is stack.lo+StackGuard normally, but can be StackPreempt to trigger a preemption.
	// stackguard1 is the stack pointer compared in the //go:systemstack stack growth prologue.
	// It is stack.lo+StackGuard on g0 and gsignal stacks.
	// It is ~0 on other goroutine stacks, to trigger a call to morestackc (and crash).
	// Not for gccgo: stack       stack   // offset known to runtime/cgo
	// Not for gccgo: stackguard0 uintptr // offset known to liblink
	// Not for gccgo: stackguard1 uintptr // offset known to liblink

	_panic *_panic // innermost panic - offset known to liblink
	_defer *_defer // innermost defer
	m      *m      // current m; offset known to arm liblink
	// Not for gccgo: sched          gobuf
	syscallsp uintptr // if status==Gsyscall, syscallsp = sched.sp to use during gc
	syscallpc uintptr // if status==Gsyscall, syscallpc = sched.pc to use during gc
	// Not for gccgo: stktopsp       uintptr        // expected sp at top of stack, to check in traceback
	// param is a generic pointer parameter field used to pass
	// values in particular contexts where other storage for the
	// parameter would be difficult to find. It is currently used
	// in four ways:
	// 1. When a channel operation wakes up a blocked goroutine, it sets param to
	//    point to the sudog of the completed blocking operation.
	// 2. By gcAssistAlloc1 to signal back to its caller that the goroutine completed
	//    the GC cycle. It is unsafe to do so in any other way, because the goroutine's
	//    stack may have moved in the meantime.
	// 3. By debugCallWrap to pass parameters to a new goroutine because allocating a
	//    closure in the runtime is forbidden.
	// 4. When a panic is recovered and control returns to the respective frame,
	//    param may point to a savedOpenDeferState.
	param        unsafe.Pointer
	atomicstatus atomic.Uint32
	// Not for gccgo: stackLock      uint32 // sigprof/scang lock; TODO: fold in to atomicstatus
	goid        int64
	schedlink   guintptr
	waitsince   int64      // approx time when the g become blocked
	waitreason  waitReason // if status==Gwaiting
	preempt     bool       // preemption signal, duplicates stackguard0 = stackpreempt
	preemptStop bool       // transition to _Gpreempted on preemption; otherwise, just deschedule
	// Not for gccgo: preemptShrink bool // shrink stack at synchronous safe point
	// asyncSafePoint is set if g is stopped at an asynchronous
	// safe point. This means there are frames on the stack
	// without precise pointer information.
	asyncSafePoint bool

	paniconfault bool // panic (instead of crash) on unexpected fault address
	preemptscan  bool // preempted g does scan for gc
	gcscandone   bool // g has scanned stack; protected by _Gscan bit in status
	throwsplit   bool // must not split stack

	gcScannedSyscallStack bool // gccgo specific; see scanSyscallStack

	// activeStackChans indicates that there are unlocked channels
	// pointing into this goroutine's stack. If true, stack
	// copying needs to acquire channel locks to protect these
	// areas of the stack.
	activeStackChans bool
	// parkingOnChan indicates that the goroutine is about to
	// park on a chansend or chanrecv. Used to signal an unsafe point
	// for stack shrinking.
	parkingOnChan atomic.Bool
	// inMarkAssist indicates whether the goroutine is in mark assist.
	// Used by the execution tracer.
	inMarkAssist bool
	coroexit     bool // argument to coroswitch_m

	raceignore      int8     // ignore race detection events
	sysblocktraced  bool     // StartTrace has emitted EvGoInSyscall about this goroutine
	tracking        bool     // whether we're tracking this G for sched latency statistics
	trackingSeq     uint8    // used to decide whether to track this G
	trackingStamp   int64    // timestamp of when the G last started being tracked
	runnableTime    int64    // the amount of time spent runnable, cleared when running, only used when tracking
	sysexitticks    int64    // cputicks when syscall has returned (for tracing)
	traceseq        uint64   // trace event sequencer
	tracelastp      puintptr // last P emitted an event for this goroutine
	lockedm         muintptr
	fipsOnlyBypass  bool
	ditWanted       bool // set if g wants to be executed with DIT enabled
	syncSafePoint   bool // set if g is stopped at a synchronous safe point.
	runningCleanups atomic.Bool
	sig             uint32
	secret          int32 // current nesting of runtime/secret.Do calls.
	writebuf        []byte
	sigcode0        uintptr
	sigcode1        uintptr
	sigpc           uintptr
	parentGoid      uint64          // goid of goroutine that created this goroutine
	gopc            uintptr         // pc of go statement that created this goroutine
	ancestors       *[]ancestorInfo // ancestor information goroutine(s) that created this goroutine (only used if debug.tracebackancestors)
	startpc         uintptr         // pc of goroutine function
	racectx         uintptr
	waiting         *sudog // sudog structures this g is waiting on (that have a valid elem ptr); in lock order
	// Not for gccgo: cgoCtxt        []uintptr      // cgo traceback context
	labels     unsafe.Pointer // profiler labels
	timer      *timer         // cached timer for time.Sleep
	sleepWhen  int64          // when to sleep until
	selectDone atomic.Uint32  // are we participating in a select and did someone win the race?

	// goroutineProfiled indicates the status of this goroutine's stack for the
	// current in-progress goroutine profile
	goroutineProfiled goroutineProfileStateHolder

	coroarg *coro // argument during coroutine transfers
	bubble  *synctestBubble

	// xRegs stores the extended register state if this G has been
	// asynchronously preempted.
	xRegs xRegPerG

	// Per-G tracer state.
	trace gTraceState

	// Per-G GC state

	// gcAssistBytes is this G's GC assist credit in terms of
	// bytes allocated. If this is positive, then the G has credit
	// to allocate gcAssistBytes bytes without assisting. If this
	// is negative, then the G must correct this by performing
	// scan work. We track this in bytes to make it fast to update
	// and check for debt in the malloc hot path. The assist ratio
	// determines how this corresponds to scan work debt.
	gcAssistBytes int64

	// Remaining fields are specific to gccgo.

	fipsIndicator uint8 // FIPS 140 service indicator (crypto/internal/fips140)

	exception unsafe.Pointer // current exception being thrown
	isforeign bool           // whether current exception is not from Go

	// When using split-stacks, these fields holds the results of
	// __splitstack_find while executing a syscall. These are used
	// by the garbage collector to scan the goroutine's stack.
	//
	// When not using split-stacks, g0 stacks are allocated by the
	// libc and other goroutine stacks are allocated by malg.
	// gcstack: unused (sometimes cleared)
	// gcstacksize: g0: 0; others: size of stack
	// gcnextsegment: unused
	// gcnextsp: current SP while executing a syscall
	// gcinitialsp: g0: top of stack; others: start of stack memory
	// gcnextsp2: current secondary stack pointer (if present)
	// gcinitialsp2: start of secondary stack (if present)
	gcstack       uintptr
	gcstacksize   uintptr
	gcnextsegment uintptr
	gcnextsp      uintptr
	gcinitialsp   unsafe.Pointer
	gcnextsp2     uintptr
	gcinitialsp2  unsafe.Pointer

	// gcregs holds the register values while executing a syscall.
	// This is set by getcontext and scanned by the garbage collector.
	gcregs g_ucontext_t

	entry    func(unsafe.Pointer) // goroutine function to run
	entryfn  uintptr              // function address passed to __go_go
	entrysp  uintptr              // the stack pointer of the outermost Go frame
	fromgogo bool                 // whether entered from gogo function

	scanningself bool // whether goroutine is scanning its own stack

	scang   uintptr // the g that wants to scan this g's stack (uintptr to avoid write barrier)
	scangcw uintptr // gc worker for scanning stack (uintptr to avoid write barrier)

	isSystemGoroutine    bool // whether goroutine is a "system" goroutine
	isFinalizerGoroutine bool // whether goroutine is the finalizer goroutine

	deferring          bool // whether we are running a deferred function
	goexiting          bool // whether we are running Goexit
	ranCgocallBackDone bool // whether we deferred CgocallBackDone

	traceback uintptr // stack traceback buffer

	context      g_ucontext_t // saved context for setcontext
	stackcontext [10]uintptr  // split-stack context

	// valgrindStackID is used to track what memory is used for stacks when a program is
	// built with the "valgrind" build tag, otherwise it is unused.
	valgrindStackID uintptr
}

// gTrackingPeriod is the number of transitions out of _Grunning between
// latency tracking runs.
const gTrackingPeriod = 8

const (
	// tlsSlots is the number of pointer-sized slots reserved for TLS on some platforms,
	// like Windows.
	tlsSlots = 6
	tlsSize  = tlsSlots * goarch.PtrSize
)

// Values for m.freeWait.
const (
	freeMStack = 0 // M done, free stack and reference.
	freeMRef   = 1 // M done, free reference.
	freeMWait  = 2 // M still in use.
)

type m struct {
	g0 *g // goroutine with scheduling stack
	// Not for gccgo: morebuf gobuf  // gobuf arg to morestack
	// Not for gccgo: divmod  uint32 // div/mod denominator for arm - known to liblink

	// Fields not known to debuggers.
	procid  uint64 // for debuggers, but offset not hard-coded
	gsignal *g     // signal-handling g
	// Not for gccgo: goSigStack    gsignalStack // Go-allocated signal handling stack
	sigmask sigset // storage for saved signal mask
	// Not for gccgo: tls           [tlsSlots]uintptr   // thread-local storage (for x86 extern register)
	mstartfn     func()
	curg         *g       // current running goroutine
	caughtsig    guintptr // goroutine running during fatal signal
	p            puintptr // attached p for executing go code (nil if not executing go code)
	nextp        puintptr
	oldp         puintptr // the p that was attached before executing a syscall
	id           int64
	mallocing    int32
	throwing     throwType
	preemptoff   string // if != "", keep curg running on this m
	locks        int32
	dying        int32
	profilehz    int32
	spinning     bool // m is out of work and is actively looking for work
	blocked      bool // m is blocked on a note
	newSigstack  bool // minit on C thread called sigaltstack
	printlock    int8
	incgo        bool          // m is executing a cgo call
	isextra      bool          // m is an extra m
	isExtraInC   bool          // m is an extra m that is not executing Go code
	isExtraInSig bool          // m is an extra m in a signal handler
	freeWait     atomic.Uint32 // Whether it is safe to free g0 and delete m (one of freeMRef, freeMStack, freeMWait)
	fastrand     uint64
	needextram   bool
	traceback    uint8
	ncgocall     uint64 // number of cgo calls in total
	ncgo         int32  // number of cgo calls currently in progress
	// Not for gccgo: cgoCallersUse uint32      // if non-zero, cgoCallers in use temporarily
	// Not for gccgo: cgoCallers    *cgoCallers // cgo traceback if crashing in cgo call
	park        note
	alllink     *m // on allm
	schedlink   muintptr
	idleNode    listNodeManual
	lockedg     guintptr
	createstack [32]location // stack that created this thread.
	lockedExt   uint32       // tracking for external LockOSThread
	lockedInt   uint32       // tracking for internal lockOSThread
	nextwaitm   muintptr     // next m waiting for lock
	ditEnabled  bool         // set if DIT is currently enabled on this M

	mLockProfile mLockProfile // fields relating to runtime.lock contention
	profStack    []uintptr    // used for memory/block/mutex stack traces

	// wait* are used to carry arguments from gopark into park_m, because
	// there's no stack to put them on. That is their sole purpose.
	waitunlockf          func(*g, unsafe.Pointer) bool
	waitlock             unsafe.Pointer
	waittraceev          byte
	waittraceskip        int
	waitTraceBlockReason traceBlockReason
	waitTraceSkip        int
	startingtrace        bool

	syscalltick uint32
	freelink    *m // on sched.freem
	trace       mTraceState

	// these are here because they are too large to be on the stack
	// of low-level NOSPLIT functions.
	// Not for gccgo: libcall   libcall
	// Not for gccgo: libcallpc uintptr // for cpu profiler
	// Not for gccgo: libcallsp uintptr
	// Not for gccgo: libcallg  guintptr
	// Not for gccgo: syscall   libcall // stores syscall parameters on windows

	// preemptGen counts the number of completed preemption
	// signals. This is used to detect when a preemption is
	// requested, but fails.
	preemptGen atomic.Uint32

	// Whether this is a pending preemption signal on this M.
	signalPending atomic.Uint32

	// A snapshot of allp, taken by snapshotAllp for use after
	// dropping the P (see proc.go). The M holds a reference on the
	// snapshot to keep the backing array alive.
	allpSnapshot []*p

	// gccgo has no pclntab, so no pcvalue lookup cache.

	dlogPerM

	mOS

	chacha8   chacha8rand.State
	cheaprand uint64

	// Up to 10 locks held by this m, maintained by the lock ranking code.
	locksHeldLen int
	locksHeld    [10]heldLockInfo

	// Remaining fields are specific to gccgo.

	gsignalstack     unsafe.Pointer // stack for gsignal
	gsignalstacksize uintptr

	dropextram bool // drop after call is done
	exiting    bool // thread is exiting

	scannote note // synchonization for signal-based stack scanning
}

// mWeakPointer is a "weak" pointer to an M. A weak pointer for each M is
// available as m.self. Users may copy mWeakPointer arbitrarily, and get will
// return the M if it is still live, or nil after mexit.
//
// The zero value is treated as a nil pointer.
//
// Note that get may race with M exit. A successful get will keep the m object
// alive, but the M itself may be exited and thus not actually usable.
type mWeakPointer struct {
	m *atomic.Pointer[m]
}

func newMWeakPointer(mp *m) mWeakPointer {
	w := mWeakPointer{m: new(atomic.Pointer[m])}
	w.m.Store(mp)
	return w
}

func (w mWeakPointer) get() *m {
	if w.m == nil {
		return nil
	}
	return w.m.Load()
}

// clear sets the weak pointer to nil. It cannot be used on zero value
// mWeakPointers.
func (w mWeakPointer) clear() {
	w.m.Store(nil)
}

type p struct {
	id          int32
	status      uint32 // one of pidle/prunning/...
	link        puintptr
	schedtick   uint32     // incremented on every scheduler call
	syscalltick uint32     // incremented on every system call
	sysmontick  sysmontick // last tick observed by sysmon
	m           muintptr   // back-link to associated m (nil if idle)
	mcache      *mcache
	pcache      pageCache
	raceprocctx uintptr

	// oldm is the previous m this p ran on.
	//
	// We are not assosciated with this m, so we have no control over its
	// lifecycle. This value is an m.self object which points to the m
	// until the m exits.
	//
	// Note that this m may be idle, running, or exiting. It should only be
	// used with mgetSpecific, which will take ownership of the m only if
	// it is idle.
	oldm mWeakPointer

	deferpool    []*_defer // pool of available defer structs (see panic.go)
	deferpoolbuf [32]*_defer

	// Cache of goroutine ids, amortizes accesses to runtime·sched.goidgen.
	goidcache    uint64
	goidcacheend uint64

	// Queue of runnable goroutines. Accessed without lock.
	runqhead uint32
	runqtail uint32
	runq     [256]guintptr
	// runnext, if non-nil, is a runnable G that was ready'd by
	// the current G and should be run next instead of what's in
	// runq if there's time remaining in the running G's time
	// slice. It will inherit the time left in the current time
	// slice. If a set of goroutines is locked in a
	// communicate-and-wait pattern, this schedules that set as a
	// unit and eliminates the (potentially large) scheduling
	// latency that otherwise arises from adding the ready'd
	// goroutines to the end of the run queue.
	//
	// Note that while other P's may atomically CAS this to zero,
	// only the owner P can CAS it to a valid G.
	runnext guintptr

	// Available G's (status == Gdead)
	gFree gList

	sudogcache []*sudog
	sudogbuf   [128]*sudog

	// Cache of mspan objects from the heap.
	mspancache struct {
		// We need an explicit length here because this field is used
		// in allocation codepaths where write barriers are not allowed,
		// and eliminating the write barrier/keeping it eliminated from
		// slice updates is tricky, more so than just managing the length
		// ourselves.
		len int
		buf [128]*mspan
	}

	// Cache of a single pinner object to reduce allocations from repeated
	// pinner creation.
	pinnerCache *pinner

	tracebuf traceBufPtr

	// traceSweep indicates the sweep events should be traced.
	// This is used to defer the sweep start event until a span
	// has actually been swept.
	traceSweep bool
	// traceSwept and traceReclaimed track the number of bytes
	// swept and reclaimed by sweeping in the current sweep loop.
	traceSwept, traceReclaimed uintptr

	palloc persistentAlloc // per-P to avoid mutex

	// Per-P GC state
	gcAssistTime         int64        // Nanoseconds in assistAlloc
	gcFractionalMarkTime atomic.Int64 // Nanoseconds in fractional mark worker

	// limiterEvent tracks events for the GC CPU limiter.
	limiterEvent limiterEvent

	// gcMarkWorkerMode is the mode for the next mark worker to run in.
	// That is, this is used to communicate with the worker goroutine
	// selected for immediate execution by
	// gcController.findRunnableGCWorker. When scheduling other goroutines,
	// this field must be set to gcMarkWorkerNotWorker.
	gcMarkWorkerMode gcMarkWorkerMode
	// gcMarkWorkerStartTime is the nanotime() at which the most recent
	// mark worker started.
	gcMarkWorkerStartTime int64

	// nextGCMarkWorker is the next mark worker to run. This may be set
	// during start-the-world to assign a worker to this P. The P runs this
	// worker on the next call to gcController.findRunnableGCWorker. If the
	// P runs something else or stops, it must release this worker via
	// gcController.releaseNextGCMarkWorker.
	//
	// See comment in gcBgMarkWorker about the lifetime of
	// gcBgMarkWorkerNode.
	//
	// Only accessed by this P or during STW.
	nextGCMarkWorker *gcBgMarkWorkerNode

	// gcw is this P's GC work buffer cache. The work buffer is
	// filled by write barriers, drained by mutator assists, and
	// disposed on certain GC state transitions.
	gcw gcWork

	// wbBuf is this P's GC write barrier buffer.
	//
	// TODO: Consider caching this in the running G.
	wbBuf wbBuf

	runSafePointFn uint32 // if 1, run sched.safePointFn at next safe point

	// statsSeq is a counter indicating whether this P is currently
	// writing any stats. Its value is even when not, odd when it is.
	statsSeq atomic.Uint32

	// Timer heap.
	timers timers

	// gccgo does not implement Go 1.25 cleanups (runtime.AddCleanup), so
	// the gc-1.25 per-P cleanup block and queued-count fields are omitted.

	// maxStackScanDelta accumulates the amount of stack space held by
	// live goroutines (i.e. those eligible for stack scanning).
	// Flushed to gcController.maxStackScan once maxStackScanSlack
	// or -maxStackScanSlack is reached.
	maxStackScanDelta int64

	// gc-time statistics about current goroutines
	// Note that this differs from maxStackScan in that this
	// accumulates the actual stack observed to be used at GC time (hi - sp),
	// not an instantaneous measure of the total stack size that might need
	// to be scanned (hi - lo).
	scannedStackSize uint64 // stack size of goroutines scanned by this P
	scannedStacks    uint64 // number of goroutines scanned by this P

	// preempt is set to indicate that this P should be enter the
	// scheduler ASAP (regardless of what G is running on it).
	preempt bool

	// gcStopTime is the nanotime timestamp that this P last entered _Pgcstop.
	gcStopTime int64

	// goroutinesCreated is the total count of goroutines created by this P.
	goroutinesCreated uint64

	// xRegs is the per-P extended register state used by asynchronous
	// preemption. This is an empty struct on platforms that don't use extended
	// register state.
	xRegs xRegPerP

	// Padding is no longer needed. False sharing is now not a worry because p is large enough
	// that its size class is an integer multiple of the cache line size (for any of our architectures).
}

type schedt struct {
	goidgen    atomic.Uint64
	lastpoll   atomic.Int64 // time of last network poll, 0 if currently polling
	pollUntil  atomic.Int64 // time to which current poll is sleeping
	pollingNet atomic.Int32 // 1 if some P doing non-blocking network poll

	lock mutex

	// When increasing nmidle, nmidlelocked, nmsys, or nmfreed, be
	// sure to call checkdead().

	midle        listHeadManual // idle m's waiting for work
	nmidle       int32          // number of idle m's waiting for work
	nmidlelocked int32          // number of locked m's waiting for work
	mnext        int64          // number of m's that have been created and next M ID
	maxmcount    int32          // maximum number of m's allowed (or die)
	nmsys        int32          // number of system m's not counted for deadlock
	nmfreed      int64          // cumulative number of freed m's

	ngsys        atomic.Int32 // number of system goroutines
	nGsyscallNoP atomic.Int32 // number of goroutines in syscalls without a P but whose M is not isExtraInC

	pidle        puintptr // idle p's
	npidle       atomic.Int32
	nmspinning   atomic.Int32  // See "Worker thread parking/unparking" comment in proc.go.
	needspinning atomic.Uint32 // See "Delicate dance" comment in proc.go. Boolean. Must hold sched.lock to set to 1.

	// Global runnable queue.
	runq gQueue

	// disable controls selective disabling of the scheduler.
	//
	// Use schedEnableUser to control this.
	//
	// disable is protected by sched.lock.
	disable struct {
		// user disables scheduling of user goroutines.
		user     bool
		runnable gQueue // pending runnable Gs
	}

	// Global cache of dead G's.
	gFree struct {
		lock mutex
		list gList // Gs
		n    int32
	}

	// Central cache of sudog structs.
	sudoglock  mutex
	sudogcache *sudog

	// Central pool of available defer structs.
	deferlock mutex
	deferpool *_defer

	// freem is the list of m's waiting to be freed when their
	// m.exited is set. Linked through m.freelink.
	freem *m

	gcwaiting  atomic.Bool // gc is waiting to run
	stopwait   int32
	stopnote   note
	sysmonwait atomic.Bool
	sysmonnote note

	// safePointFn should be called on each P at the next GC
	// safepoint if p.runSafePointFn is set.
	safePointFn   func(*p)
	safePointWait int32
	safePointNote note

	profilehz int32 // cpu profiling rate

	procresizetime int64 // nanotime() of last change to gomaxprocs
	totaltime      int64 // ∫gomaxprocs dt up to procresizetime

	customGOMAXPROCS bool // GOMAXPROCS was manually set from the environment or runtime.GOMAXPROCS

	// sysmonlock protects sysmon's actions on the runtime.
	//
	// Acquire and hold this mutex to block sysmon from interacting
	// with the rest of the runtime.
	sysmonlock mutex

	_ uint32 // for alignment

	// timeToRun is a distribution of scheduling latencies, defined
	// as the sum of time a G spends in the _Grunnable state before
	// it transitions to _Grunning.
	timeToRun timeHistogram

	// idleTime is the total CPU time Ps have "spent" idle.
	//
	// Reset on each GC cycle.
	idleTime atomic.Int64

	// totalMutexWaitTime is the sum of time goroutines have spent in _Gwaiting
	// with a waitreason of the form waitReasonSync{RW,}Mutex{R,}Lock.
	totalMutexWaitTime atomic.Int64

	// stwStoppingTimeGC/Other are distributions of stop-the-world stopping
	// latencies, defined as the time taken by stopTheWorldWithSema to get
	// all Ps to stop. stwStoppingTimeGC covers all GC-related STWs,
	// stwStoppingTimeOther covers the others.
	stwStoppingTimeGC    timeHistogram
	stwStoppingTimeOther timeHistogram

	// stwTotalTimeGC/Other are distributions of stop-the-world total
	// latencies, defined as the total time from stopTheWorldWithSema to
	// startTheWorldWithSema. This is a superset of
	// stwStoppingTimeGC/Other. stwTotalTimeGC covers all GC-related STWs,
	// stwTotalTimeOther covers the others.
	stwTotalTimeGC    timeHistogram
	stwTotalTimeOther timeHistogram

	// totalRuntimeLockWaitTime (plus the value of lockWaitTime on each M in
	// allm) is the sum of time goroutines have spent in _Grunnable and with an
	// M, but waiting for locks within the runtime. This field stores the value
	// for Ms that have exited.
	totalRuntimeLockWaitTime atomic.Int64

	// goroutinesCreated (plus the value of goroutinesCreated on each P in allp)
	// is the sum of all goroutines created by the program.
	goroutinesCreated atomic.Uint64
}

// Values for the flags field of a sigTabT.
const (
	_SigNotify   = 1 << iota // let signal.Notify have signal, even if from kernel
	_SigKill                 // if signal.Notify doesn't take it, exit quietly
	_SigThrow                // if signal.Notify doesn't take it, exit loudly
	_SigPanic                // if the signal is from the kernel, panic
	_SigDefault              // if the signal isn't explicitly requested, don't monitor it
	_SigGoExit               // cause all runtime procs to exit (only used on Plan 9).
	_SigSetStack             // Don't explicitly install handler, but add SA_ONSTACK to existing libc handler
	_SigUnblock              // always unblock; see blockableSig
	_SigIgn                  // _SIG_DFL action is to ignore the signal
)

// Lock-free stack node.
// Also known to export_test.go.
type lfnode struct {
	next    uint64
	pushcnt uintptr
}

type forcegcstate struct {
	lock mutex
	g    *g
	idle atomic.Bool
}

// startupRandomData holds random bytes initialized at startup. These come from
// the ELF AT_RANDOM auxiliary vector.
var startupRandomData []byte

// extendRandom extends the random numbers in r[:n] to the whole slice r.
// Treats n<0 as n==0.
func extendRandom(r []byte, n int) {
	if n < 0 {
		n = 0
	}
	for n < len(r) {
		// Extend random bits using hash function & time seed
		w := n
		if w > 16 {
			w = 16
		}
		h := memhash(unsafe.Pointer(&r[n-w]), uintptr(nanotime()), uintptr(w))
		for i := 0; i < goarch.PtrSize && n < len(r); i++ {
			r[n] = byte(h)
			n++
			h >>= 8
		}
	}
}

// A _defer holds an entry on the list of deferred calls.
// This struct must match the code in Defer_statement::defer_struct_type
// in the compiler.
// Some defers will be allocated on the stack and some on the heap.
// All defers are logically part of the stack, so write barriers to
// initialize them are not required. All defers must be manually scanned,
// and for heap defers, marked.
type _defer struct {
	// The next entry in the stack.
	link *_defer

	// The stack variable for the function which called this defer
	// statement.  This is set to true if we are returning from
	// that function, false if we are panicing through it.
	frame *bool

	// The value of the panic stack when this function is
	// deferred.  This function can not recover this value from
	// the panic stack.  This can happen if a deferred function
	// has a defer statement itself.
	panicStack *_panic

	// The panic that caused the defer to run. This is used to
	// discard panics that have already been handled.
	_panic *_panic

	// The function to call.
	pfn uintptr

	// The argument to pass to the function.
	arg unsafe.Pointer

	// The return address that a recover thunk matches against.
	// This is set by __go_set_defer_retaddr which is called by
	// the thunks created by defer statements.
	retaddr uintptr

	// Set to true if a function created by reflect.MakeFunc is
	// permitted to recover.  The return address of such a
	// function function will be somewhere in libffi, so __retaddr
	// is not useful.
	makefunccanrecover bool

	// Whether the _defer is heap allocated.
	heap bool
}

// panics
// This is the gccgo version.
type _panic struct {
	// The next entry in the stack.
	link *_panic

	// The value associated with this panic.
	arg interface{}

	// Whether this panic has been recovered.
	recovered bool

	// Whether this panic was recovered and then re-raised (panicked
	// again) with the same value, so that printing can skip the
	// duplicate. Used only by preprintpanics/printpanics.
	repanicked bool

	// Whether this panic was pushed on the stack because of an
	// exception thrown in some other language.
	isforeign bool

	// Whether this panic was already seen by a deferred function
	// which called panic again.
	aborted bool

	// Whether this panic was created for goexit.
	goexit bool
}

const (
	_TraceRuntimeFrames = 1 << iota // include frames for internal runtime functions.
	_TraceTrap                      // the initial PC, SP are from a trap, not a return PC from a call
	_TraceJumpStack                 // if traceback is on a systemstack, resume trace at g that called into it
)

// The maximum number of frames we print for a traceback
const _TracebackMaxFrames = 100

// ancestorInfo records details of where a goroutine was started.
type ancestorInfo struct {
	pcs  []uintptr // pcs from the stack of this goroutine
	goid int64     // goroutine id of this goroutine; original goroutine possibly dead
	gopc uintptr   // pc of go statement that created this goroutine
}

// A waitReason explains why a goroutine has been stopped.
// See gopark. Do not re-use waitReasons, add new ones.
type waitReason uint8

const (
	waitReasonZero                  waitReason = iota // ""
	waitReasonGCAssistMarking                         // "GC assist marking"
	waitReasonIOWait                                  // "IO wait"
	waitReasonDumpingHeap                             // "dumping heap"
	waitReasonGarbageCollection                       // "garbage collection"
	waitReasonGarbageCollectionScan                   // "garbage collection scan"
	waitReasonPanicWait                               // "panicwait"
	waitReasonGCAssistWait                            // "GC assist wait"
	waitReasonGCSweepWait                             // "GC sweep wait"
	waitReasonGCScavengeWait                          // "GC scavenge wait"
	waitReasonFinalizerWait                           // "finalizer wait"
	waitReasonForceGCIdle                             // "force gc (idle)"
	waitReasonUpdateGOMAXPROCSIdle                    // "GOMAXPROCS updater (idle)"
	waitReasonSemacquire                              // "semacquire"
	waitReasonSleep                                   // "sleep"
	waitReasonChanReceiveNilChan                      // "chan receive (nil chan)"
	waitReasonChanSendNilChan                         // "chan send (nil chan)"
	waitReasonSelectNoCases                           // "select (no cases)"
	waitReasonSelect                                  // "select"
	waitReasonChanReceive                             // "chan receive"
	waitReasonChanSend                                // "chan send"
	waitReasonSyncCondWait                            // "sync.Cond.Wait"
	waitReasonSyncMutexLock                           // "sync.Mutex.Lock"
	waitReasonSyncRWMutexRLock                        // "sync.RWMutex.RLock"
	waitReasonSyncRWMutexLock                         // "sync.RWMutex.Lock"
	waitReasonSyncWaitGroupWait                       // "sync.WaitGroup.Wait"
	waitReasonTraceReaderBlocked                      // "trace reader (blocked)"
	waitReasonWaitForGCCycle                          // "wait for GC cycle"
	waitReasonGCWorkerIdle                            // "GC worker (idle)"
	waitReasonGCWorkerActive                          // "GC worker (active)"
	waitReasonPreempted                               // "preempted"
	waitReasonDebugCall                               // "debug call"
	waitReasonGCMarkTermination                       // "GC mark termination"
	waitReasonStoppingTheWorld                        // "stopping the world"
	waitReasonFlushProcCaches                         // "flushing proc caches"
	waitReasonTraceGoroutineStatus                    // "trace goroutine status"
	waitReasonTraceProcStatus                         // "trace proc status"
	waitReasonPageTraceFlush                          // "page trace flush"
	waitReasonCoroutine                               // "coroutine"
	waitReasonGCWeakToStrongWait                      // "GC weak to strong wait"
	waitReasonSynctestRun                             // "synctest.Run"
	waitReasonSynctestWait                            // "synctest.Wait"
	waitReasonSynctestChanReceive                     // "chan receive (durable)"
	waitReasonSynctestChanSend                        // "chan send (durable)"
	waitReasonSynctestSelect                          // "select (durable)"
	waitReasonSynctestWaitGroupWait                   // "sync.WaitGroup.Wait (durable)"
	waitReasonCleanupWait                             // "cleanup wait"
)

var waitReasonStrings = [...]string{
	waitReasonZero:                  "",
	waitReasonGCAssistMarking:       "GC assist marking",
	waitReasonIOWait:                "IO wait",
	waitReasonChanReceiveNilChan:    "chan receive (nil chan)",
	waitReasonChanSendNilChan:       "chan send (nil chan)",
	waitReasonDumpingHeap:           "dumping heap",
	waitReasonGarbageCollection:     "garbage collection",
	waitReasonGarbageCollectionScan: "garbage collection scan",
	waitReasonPanicWait:             "panicwait",
	waitReasonSelect:                "select",
	waitReasonSelectNoCases:         "select (no cases)",
	waitReasonGCAssistWait:          "GC assist wait",
	waitReasonGCSweepWait:           "GC sweep wait",
	waitReasonGCScavengeWait:        "GC scavenge wait",
	waitReasonChanReceive:           "chan receive",
	waitReasonChanSend:              "chan send",
	waitReasonFinalizerWait:         "finalizer wait",
	waitReasonForceGCIdle:           "force gc (idle)",
	waitReasonUpdateGOMAXPROCSIdle:  "GOMAXPROCS updater (idle)",
	waitReasonSemacquire:            "semacquire",
	waitReasonSleep:                 "sleep",
	waitReasonSyncCondWait:          "sync.Cond.Wait",
	waitReasonSyncMutexLock:         "sync.Mutex.Lock",
	waitReasonSyncRWMutexRLock:      "sync.RWMutex.RLock",
	waitReasonSyncRWMutexLock:       "sync.RWMutex.Lock",
	waitReasonSyncWaitGroupWait:     "sync.WaitGroup.Wait",
	waitReasonTraceReaderBlocked:    "trace reader (blocked)",
	waitReasonWaitForGCCycle:        "wait for GC cycle",
	waitReasonGCWorkerIdle:          "GC worker (idle)",
	waitReasonGCWorkerActive:        "GC worker (active)",
	waitReasonPreempted:             "preempted",
	waitReasonDebugCall:             "debug call",
	waitReasonGCMarkTermination:     "GC mark termination",
	waitReasonStoppingTheWorld:      "stopping the world",
	waitReasonFlushProcCaches:       "flushing proc caches",
	waitReasonTraceGoroutineStatus:  "trace goroutine status",
	waitReasonTraceProcStatus:       "trace proc status",
	waitReasonPageTraceFlush:        "page trace flush",
	waitReasonCoroutine:             "coroutine",
	waitReasonGCWeakToStrongWait:    "GC weak to strong wait",
	waitReasonSynctestRun:           "synctest.Run",
	waitReasonSynctestWait:          "synctest.Wait",
	waitReasonSynctestChanReceive:   "chan receive (durable)",
	waitReasonSynctestChanSend:      "chan send (durable)",
	waitReasonSynctestSelect:        "select (durable)",
	waitReasonSynctestWaitGroupWait: "sync.WaitGroup.Wait (durable)",
	waitReasonCleanupWait:           "cleanup wait",
}

func (w waitReason) String() string {
	if w < 0 || w >= waitReason(len(waitReasonStrings)) {
		return "unknown wait reason"
	}
	return waitReasonStrings[w]
}

// isMutexWait returns true if the goroutine is blocked because of
// sync.Mutex.Lock or sync.RWMutex.[R]Lock.
//
//go:nosplit
func (w waitReason) isMutexWait() bool {
	return w == waitReasonSyncMutexLock ||
		w == waitReasonSyncRWMutexRLock ||
		w == waitReasonSyncRWMutexLock
}

// isSyncWait returns true if the goroutine is blocked because of
// sync library primitive operations.
//
//go:nosplit
func (w waitReason) isSyncWait() bool {
	return waitReasonSyncCondWait <= w && w <= waitReasonSyncWaitGroupWait
}

// isChanWait is true if the goroutine is blocked because of non-nil
// channel operations or a select statement with at least one case.
//
//go:nosplit
func (w waitReason) isChanWait() bool {
	return w == waitReasonSelect ||
		w == waitReasonChanReceive ||
		w == waitReasonChanSend
}

func (w waitReason) isWaitingForSuspendG() bool {
	return isWaitingForSuspendG[w]
}

// isWaitingForSuspendG indicates that a goroutine is only entering _Gwaiting and
// setting a waitReason because it needs to be able to let the suspendG
// (used by the GC and the execution tracer) take ownership of its stack.
// The G is always actually executing on the system stack in these cases.
//
// TODO(mknyszek): Consider replacing this with a new dedicated G status.
var isWaitingForSuspendG = [len(waitReasonStrings)]bool{
	waitReasonStoppingTheWorld:      true,
	waitReasonGCMarkTermination:     true,
	waitReasonGarbageCollection:     true,
	waitReasonGarbageCollectionScan: true,
	waitReasonTraceGoroutineStatus:  true,
	waitReasonTraceProcStatus:       true,
	waitReasonPageTraceFlush:        true,
	waitReasonGCAssistMarking:       true,
	waitReasonGCWorkerActive:        true,
	waitReasonFlushProcCaches:       true,
}

func (w waitReason) isIdleInSynctest() bool {
	return isIdleInSynctest[w]
}

// isIdleInSynctest indicates that a goroutine is considered idle by synctest.Wait.
var isIdleInSynctest = [len(waitReasonStrings)]bool{
	waitReasonChanReceiveNilChan:    true,
	waitReasonChanSendNilChan:       true,
	waitReasonSelectNoCases:         true,
	waitReasonSleep:                 true,
	waitReasonSyncCondWait:          true,
	waitReasonSynctestWaitGroupWait: true,
	waitReasonCoroutine:             true,
	waitReasonSynctestRun:           true,
	waitReasonSynctestWait:          true,
	waitReasonSynctestChanReceive:   true,
	waitReasonSynctestChanSend:      true,
	waitReasonSynctestSelect:        true,
}

var (
	// Linked-list of all Ms. Written under sched.lock, read atomically.
	allm *m

	gomaxprocs    int32
	numCPUStartup int32
	forcegc       forcegcstate
	sched         schedt
	newprocs      int32
)

var (
	// allpLock protects P-less reads and size changes of allp, idlepMask,
	// and timerpMask, and all writes to allp.
	allpLock mutex

	// len(allp) == gomaxprocs; may change at safe points, otherwise
	// immutable.
	allp []*p

	// Bitmask of Ps in _Pidle list, one bit per P. Reads and writes must
	// be atomic. Length may change at safe points.
	//
	// Each P must update only its own bit. In order to maintain
	// consistency, a P going idle must set the idle mask simultaneously with
	// updates to the idle P list under the sched.lock, otherwise a racing
	// pidleget may clear the mask before pidleput sets the mask,
	// corrupting the bitmap.
	//
	// N.B., procresize takes ownership of all Ps in stopTheWorldWithSema.
	idlepMask pMask

	// Bitmask of Ps that may have a timer, one bit per P. Reads and writes
	// must be atomic. Length may change at safe points.
	//
	// Ideally, the timer mask would be kept immediately consistent on any timer
	// operations. Unfortunately, updating a shared global data structure in the
	// timer hot path adds too much overhead in applications frequently switching
	// between no timers and some timers.
	//
	// As a compromise, the timer mask is updated only on pidleget / pidleput. A
	// running P (returned by pidleget) may add a timer at any time, so its mask
	// must be set. An idle P (passed to pidleput) cannot add new timers while
	// idle, so if it has no timers at that time, its mask may be cleared.
	//
	// Thus, we get the following effects on timer-stealing in findRunnable:
	//
	//   - Idle Ps with no timers when they go idle are never checked in findRunnable
	//     (for work- or timer-stealing; this is the ideal case).
	//   - Running Ps must always be checked.
	//   - Idle Ps whose timers are stolen must continue to be checked until they run
	//     again, even after timer expiration.
	//
	// When the P starts running again, the mask should be set, as a timer may be
	// added at any time.
	//
	// TODO(prattmic): Additional targeted updates may improve the above cases.
	// e.g., updating the mask when stealing a timer.
	timerpMask pMask
)

var (
	// Pool of GC parked background workers. Entries are type
	// *gcBgMarkWorkerNode.
	gcBgMarkWorkerPool lfstack

	// Total number of gcBgMarkWorker goroutines. Protected by worldsema.
	gcBgMarkWorkerCount int32

	support_aes bool
)

// Set by the linker so the runtime can determine the buildmode.
var (
	islibrary bool // -buildmode=c-shared
	isarchive bool // -buildmode=c-archive
)

// Types that are only used by gccgo.

// g_ucontext_t is a Go version of the C ucontext_t type, used by getcontext.
// _sizeof_ucontext_t is defined by mkrsysinfo.sh from <ucontext.h>.
// On some systems getcontext and friends require a value that is
// aligned to a 16-byte boundary.  We implement this by increasing the
// required size and picking an appropriate offset when we use the
// array.
type g_ucontext_t [(_sizeof_ucontext_t + 15) / unsafe.Sizeof(uintptr(0))]uintptr

// sigset is the Go version of the C type sigset_t.
// _sigset_t is defined by the Makefile from <signal.h>.
type sigset _sigset_t

// getMemstats returns a pointer to the internal memstats variable,
// for C code.
//go:linkname getMemstats
func getMemstats() *mstats {
	return &memstats
}
