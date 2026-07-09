// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Malloc profiling.
// Patterned after tcmalloc's algorithms; shorter code.

package runtime

import (
	"internal/abi"
	"internal/profilerecord"
	"internal/runtime/atomic"
	"internal/runtime/sys"
	"unsafe"
)

// logicalStackSentinel is a sentinel value at pcBuf[0] signifying that
// pcBuf[1:] holds a logical stack requiring no further processing. In the
// gc runtime this is defined by the execution tracer (trace2stack.go),
// which gccgo does not build; define it here so the mutex-profile code
// compiles.
const logicalStackSentinel = ^uintptr(0)

// For gofrontend, use go:linkname for blockevent so that
// runtime/pprof/pprof_test can call it.
//go:linkname blockevent

// NOTE(rsc): Everything here could use cas if contention became an issue.
var (
	// profInsertLock protects changes to the start of all *bucket linked lists
	profInsertLock mutex
	// profBlockLock protects the contents of every blockRecord struct
	profBlockLock mutex
	// profMemActiveLock protects the active field of every memRecord struct
	profMemActiveLock mutex
	// profMemFutureLock is a set of locks that protect the respective elements
	// of the future array of every memRecord struct
	profMemFutureLock [len(memRecord{}.future)]mutex
)

// All memory allocations are local and do not escape outside of the profiler.
// The profiler is forbidden from referring to garbage-collected memory.

const (
	// profile types
	memProfile bucketType = 1 + iota
	blockProfile
	mutexProfile

	// a profile bucket from one of the categories above whose stack
	// trace has been fixed up / pruned.
	prunedProfile

	// size of bucket hash table
	buckHashSize = 179999

	// maxStack is the max depth of stack to record in bucket.
	// Note that it's only used internally as a guard against
	// wildly out-of-bounds slicing of the PCs that come from a
	// bucket, and it could increase in the future.
	maxStack = 32

	// maxSkip is to account for deferred inline expansion
	// when using frame pointer unwinding. We record the stack
	// with "physical" frame pointers but handle skipping "logical"
	// frames at some point after collecting the stack. So
	// we need extra space in order to avoid getting fewer than the
	// desired maximum number of frames after expansion.
	// This should be at least as large as the largest skip value
	// used for profiling; otherwise stacks may be truncated inconsistently
	maxSkip = 5

	// maxProfStackDepth is the highest valid value for debug.profstackdepth.
	// It's used for the bucket.stk func.
	// TODO(fg): can we get rid of this?
	maxProfStackDepth = 1024
)

type bucketType int

// A bucket holds per-call-stack profiling information.
// The representation is a bit sleazy, inherited from C.
// This struct defines the bucket header. It is followed in
// memory by the stack words and then the actual record
// data, either a memRecord or a blockRecord.
//
// Per-call-stack profiling information.
// Lookup by hashing call stack into a linked-list hash table.
//
// None of the fields in this bucket header are modified after
// creation, including its next and allnext links.
//
// No heap pointers.
//go:notinheap
type bucket struct {
	_       sys.NotInHeap
	next    *bucket
	allnext *bucket
	typ     bucketType // memBucket or blockBucket (includes mutexProfile)
	hash    uintptr
	size    uintptr
	nstk    uintptr
	skip    int
}

// A memRecord is the bucket data for a bucket of type memProfile,
// part of the memory profile.
type memRecord struct {
	// The following complex 3-stage scheme of stats accumulation
	// is required to obtain a consistent picture of mallocs and frees
	// for some point in time.
	// The problem is that mallocs come in real time, while frees
	// come only after a GC during concurrent sweeping. So if we would
	// naively count them, we would get a skew toward mallocs.
	//
	// Hence, we delay information to get consistent snapshots as
	// of mark termination. Allocations count toward the next mark
	// termination's snapshot, while sweep frees count toward the
	// previous mark termination's snapshot:
	//
	//              MT          MT          MT          MT
	//             .·|         .·|         .·|         .·|
	//          .·˙  |      .·˙  |      .·˙  |      .·˙  |
	//       .·˙     |   .·˙     |   .·˙     |   .·˙     |
	//    .·˙        |.·˙        |.·˙        |.·˙        |
	//
	//       alloc → ▲ ← free
	//               ┠┅┅┅┅┅┅┅┅┅┅┅P
	//       C+2     →    C+1    →  C
	//
	//                   alloc → ▲ ← free
	//                           ┠┅┅┅┅┅┅┅┅┅┅┅P
	//                   C+2     →    C+1    →  C
	//
	// Since we can't publish a consistent snapshot until all of
	// the sweep frees are accounted for, we wait until the next
	// mark termination ("MT" above) to publish the previous mark
	// termination's snapshot ("P" above). To do this, allocation
	// and free events are accounted to *future* heap profile
	// cycles ("C+n" above) and we only publish a cycle once all
	// of the events from that cycle must be done. Specifically:
	//
	// Mallocs are accounted to cycle C+2.
	// Explicit frees are accounted to cycle C+2.
	// GC frees (done during sweeping) are accounted to cycle C+1.
	//
	// After mark termination, we increment the global heap
	// profile cycle counter and accumulate the stats from cycle C
	// into the active profile.

	// active is the currently published profile. A profiling
	// cycle can be accumulated into active once its complete.
	active memRecordCycle

	// future records the profile events we're counting for cycles
	// that have not yet been published. This is ring buffer
	// indexed by the global heap profile cycle C and stores
	// cycles C, C+1, and C+2. Unlike active, these counts are
	// only for a single cycle; they are not cumulative across
	// cycles.
	//
	// We store cycle C here because there's a window between when
	// C becomes the active cycle and when we've flushed it to
	// active.
	future [3]memRecordCycle
}

// memRecordCycle
type memRecordCycle struct {
	allocs, frees           uintptr
	alloc_bytes, free_bytes uintptr
}

// add accumulates b into a. It does not zero b.
func (a *memRecordCycle) add(b *memRecordCycle) {
	a.allocs += b.allocs
	a.frees += b.frees
	a.alloc_bytes += b.alloc_bytes
	a.free_bytes += b.free_bytes
}

// A blockRecord is the bucket data for a bucket of type blockProfile,
// which is used in blocking and mutex profiles.
type blockRecord struct {
	count  float64
	cycles int64
}

var (
	mbuckets atomic.UnsafePointer // *bucket, memory profile buckets
	bbuckets atomic.UnsafePointer // *bucket, blocking profile buckets
	xbuckets atomic.UnsafePointer // *bucket, mutex profile buckets
	buckhash atomic.UnsafePointer // *buckhashArray

	// gccgo-specific pre-symbolization buckets. These are only ever
	// accessed with profMemActiveLock / profBlockLock held (during
	// MemProfile / BlockProfile / MutexProfile), so they use plain
	// pointers rather than atomics.
	sbuckets    *bucket // pre-symbolization profile buckets (stacks fixed up)
	freebuckets *bucket // freelist of unused fixed up profile buckets

	mProfCycle mProfCycleHolder
)

type buckhashArray [buckHashSize]atomic.UnsafePointer // *bucket

const mProfCycleWrap = uint32(len(memRecord{}.future)) * (2 << 24)

// payloadOffset() returns a pointer into the part of a bucket
// containing the profile payload (skips past the bucket struct itself
// and then the stack trace).
func payloadOffset(typ bucketType, nstk uintptr) uintptr {
	if typ == prunedProfile {
		// To allow reuse of prunedProfile buckets between different
		// collections, allocate them with the max stack size (the portion
		// of the stack used will vary from trace to trace).
		nstk = maxStack
	}
	return unsafe.Sizeof(bucket{}) + uintptr(nstk)*unsafe.Sizeof(uintptr(0))
}

// mProfCycleHolder holds the global heap profile cycle number (wrapped at
// mProfCycleWrap, stored starting at bit 1), and a flag (stored at bit 0) to
// indicate whether future[cycle] in all buckets has been queued to flush into
// the active profile.
type mProfCycleHolder struct {
	value atomic.Uint32
}

// read returns the current cycle count.
func (c *mProfCycleHolder) read() (cycle uint32) {
	v := c.value.Load()
	cycle = v >> 1
	return cycle
}

// setFlushed sets the flushed flag. It returns the current cycle count and the
// previous value of the flushed flag.
func (c *mProfCycleHolder) setFlushed() (cycle uint32, alreadyFlushed bool) {
	for {
		prev := c.value.Load()
		cycle = prev >> 1
		alreadyFlushed = (prev & 0x1) != 0
		next := prev | 0x1
		if c.value.CompareAndSwap(prev, next) {
			return cycle, alreadyFlushed
		}
	}
}

// increment increases the cycle count by one, wrapping the value at
// mProfCycleWrap. It clears the flushed flag.
func (c *mProfCycleHolder) increment() {
	// We explicitly wrap mProfCycle rather than depending on
	// uint wraparound because the memRecord.future ring does not
	// itself wrap at a power of two.
	for {
		prev := c.value.Load()
		cycle := prev >> 1
		cycle = (cycle + 1) % mProfCycleWrap
		next := cycle << 1
		if c.value.CompareAndSwap(prev, next) {
			break
		}
	}
}

// newBucket allocates a bucket with the given type and number of stack entries.
func newBucket(typ bucketType, nstk int, skipCount int) *bucket {
	size := payloadOffset(typ, uintptr(nstk))
	switch typ {
	default:
		throw("invalid profile bucket type")
	case prunedProfile:
		// stack-fixed buckets are large enough to accommodate any payload.
		size += max(unsafe.Sizeof(memRecord{}), unsafe.Sizeof(blockRecord{}))
	case memProfile:
		size += unsafe.Sizeof(memRecord{})
	case blockProfile, mutexProfile:
		size += unsafe.Sizeof(blockRecord{})
	}

	b := (*bucket)(persistentalloc(size, 0, &memstats.buckhash_sys))
	b.typ = typ
	b.nstk = uintptr(nstk)
	b.skip = skipCount
	return b
}

// stk returns the slice in b holding the stack. The caller can asssume that the
// backing array is immutable.
func (b *bucket) stk() []uintptr {
	stk := (*[maxProfStackDepth]uintptr)(add(unsafe.Pointer(b), unsafe.Sizeof(*b)))
	if b.nstk > maxProfStackDepth {
		// prove that slicing works; otherwise a failure requires a P
		throw("bad profile stack count")
	}
	return stk[:b.nstk:b.nstk]
}

// mp returns the memRecord associated with the memProfile bucket b.
func (b *bucket) mp() *memRecord {
	if b.typ != memProfile && b.typ != prunedProfile {
		throw("bad use of bucket.mp")
	}
	return (*memRecord)(add(unsafe.Pointer(b), payloadOffset(b.typ, b.nstk)))
}

// bp returns the blockRecord associated with the blockProfile bucket b.
func (b *bucket) bp() *blockRecord {
	if b.typ != blockProfile && b.typ != mutexProfile && b.typ != prunedProfile {
		throw("bad use of bucket.bp")
	}
	return (*blockRecord)(add(unsafe.Pointer(b), payloadOffset(b.typ, b.nstk)))
}

// Return the bucket for stk[0:nstk], allocating new bucket if needed.
func stkbucket(typ bucketType, size uintptr, skip int, stk []uintptr, alloc bool) *bucket {
	bh := (*buckhashArray)(buckhash.Load())
	if bh == nil {
		lock(&profInsertLock)
		// check again under the lock
		bh = (*buckhashArray)(buckhash.Load())
		if bh == nil {
			bh = (*buckhashArray)(sysAlloc(unsafe.Sizeof(buckhashArray{}), &memstats.buckhash_sys, "profiler hash buckets"))
			if bh == nil {
				throw("runtime: cannot allocate memory")
			}
			buckhash.StoreNoWB(unsafe.Pointer(bh))
		}
		unlock(&profInsertLock)
	}

	// Hash stack.
	var h uintptr
	for _, pc := range stk {
		h += pc
		h += h << 10
		h ^= h >> 6
	}
	// hash in size
	h += size
	h += h << 10
	h ^= h >> 6
	// finalize
	h += h << 3
	h ^= h >> 11

	i := int(h % buckHashSize)
	// first check optimistically, without the lock
	for b := (*bucket)(bh[i].Load()); b != nil; b = b.next {
		if b.typ == typ && b.hash == h && b.size == size && eqslice(b.stk(), stk) {
			return b
		}
	}

	if !alloc {
		return nil
	}

	lock(&profInsertLock)
	// check again under the insertion lock
	for b := (*bucket)(bh[i].Load()); b != nil; b = b.next {
		if b.typ == typ && b.hash == h && b.size == size && eqslice(b.stk(), stk) {
			unlock(&profInsertLock)
			return b
		}
	}

	// Create new bucket.
	b := newBucket(typ, len(stk), skip)
	copy(b.stk(), stk)
	b.hash = h
	b.size = size

	if typ == prunedProfile {
		// gccgo-specific: pre-symbolization ("pruned") buckets are chained
		// onto the plain-pointer sbuckets list. They are only manipulated
		// with the relevant profile lock held, so no atomics are needed.
		b.next = (*bucket)(bh[i].Load())
		b.allnext = sbuckets
		sbuckets = b
		bh[i].StoreNoWB(unsafe.Pointer(b))
		unlock(&profInsertLock)
		return b
	}

	var allnext *atomic.UnsafePointer
	if typ == memProfile {
		allnext = &mbuckets
	} else if typ == mutexProfile {
		allnext = &xbuckets
	} else {
		allnext = &bbuckets
	}

	b.next = (*bucket)(bh[i].Load())
	b.allnext = (*bucket)(allnext.Load())

	bh[i].StoreNoWB(unsafe.Pointer(b))
	allnext.StoreNoWB(unsafe.Pointer(b))

	unlock(&profInsertLock)
	return b
}

func eqslice(x, y []uintptr) bool {
	if len(x) != len(y) {
		return false
	}
	for i, xi := range x {
		if xi != y[i] {
			return false
		}
	}
	return true
}

// mProf_NextCycle publishes the next heap profile cycle and creates a
// fresh heap profile cycle. This operation is fast and can be done
// during STW. The caller must call mProf_Flush before calling
// mProf_NextCycle again.
//
// This is called by mark termination during STW so allocations and
// frees after the world is started again count towards a new heap
// profiling cycle.
func mProf_NextCycle() {
	mProfCycle.increment()
}

// mProf_Flush flushes the events from the current heap profiling
// cycle into the active profile. After this it is safe to start a new
// heap profiling cycle with mProf_NextCycle.
//
// This is called by GC after mark termination starts the world. In
// contrast with mProf_NextCycle, this is somewhat expensive, but safe
// to do concurrently.
func mProf_Flush() {
	cycle, alreadyFlushed := mProfCycle.setFlushed()
	if alreadyFlushed {
		return
	}

	index := cycle % uint32(len(memRecord{}.future))
	lock(&profMemActiveLock)
	lock(&profMemFutureLock[index])
	mProf_FlushLocked(index)
	unlock(&profMemFutureLock[index])
	unlock(&profMemActiveLock)
}

// mProf_FlushLocked flushes the events from the heap profiling cycle at index
// into the active profile. The caller must hold the lock for the active profile
// (profMemActiveLock) and for the profiling cycle at index
// (profMemFutureLock[index]).
func mProf_FlushLocked(index uint32) {
	assertLockHeld(&profMemActiveLock)
	assertLockHeld(&profMemFutureLock[index])
	head := (*bucket)(mbuckets.Load())
	for b := head; b != nil; b = b.allnext {
		mp := b.mp()

		// Flush cycle C into the published profile and clear
		// it for reuse.
		mpc := &mp.future[index]
		mp.active.add(mpc)
		*mpc = memRecordCycle{}
	}
}

// mProf_PostSweep records that all sweep frees for this GC cycle have
// completed. This has the effect of publishing the heap profile
// snapshot as of the last mark termination without advancing the heap
// profile cycle.
func mProf_PostSweep() {
	// Flush cycle C+1 to the active profile so everything as of
	// the last mark termination becomes visible. *Don't* advance
	// the cycle, since we're still accumulating allocs in cycle
	// C+2, which have to become C+1 in the next mark termination
	// and so on.
	cycle := mProfCycle.read() + 1

	index := cycle % uint32(len(memRecord{}.future))
	lock(&profMemActiveLock)
	lock(&profMemFutureLock[index])
	mProf_FlushLocked(index)
	unlock(&profMemFutureLock[index])
	unlock(&profMemActiveLock)
}

// Called by malloc to record a profiled block.
func mProf_Malloc(mp *m, p unsafe.Pointer, size uintptr) {
	var stk [maxStack]uintptr
	nstk := callersRaw(stk[:])


	index := (mProfCycle.read() + 2) % uint32(len(memRecord{}.future))

	// gccgo records a raw stack here and defers the skip/symbolization
	// fixup until MemProfile time, so pass the skip count to be stored in
	// the bucket rather than applying it now.
	skip := 1
	b := stkbucket(memProfile, size, skip, stk[:nstk], true)
	mr := b.mp()
	mpc := &mr.future[index]

	lock(&profMemFutureLock[index])
	mpc.allocs++
	mpc.alloc_bytes += size
	unlock(&profMemFutureLock[index])

	// Setprofilebucket locks a bunch of other mutexes, so we call it outside of
	// the profiler locks. This reduces potential contention and chances of
	// deadlocks. Since the object must be alive during the call to
	// mProf_Malloc, it's fine to do this non-atomically.
	systemstack(func() {
		setprofilebucket(p, b)
	})
}

// Called when freeing a profiled block.
func mProf_Free(b *bucket, size uintptr) {
	index := (mProfCycle.read() + 1) % uint32(len(memRecord{}.future))

	mp := b.mp()
	mpc := &mp.future[index]

	lock(&profMemFutureLock[index])
	mpc.frees++
	mpc.free_bytes += size
	unlock(&profMemFutureLock[index])
}

var blockprofilerate uint64 // in CPU ticks

// SetBlockProfileRate controls the fraction of goroutine blocking events
// that are reported in the blocking profile. The profiler aims to sample
// an average of one blocking event per rate nanoseconds spent blocked.
//
// To include every blocking event in the profile, pass rate = 1.
// To turn off profiling entirely, pass rate <= 0.
func SetBlockProfileRate(rate int) {
	var r int64
	if rate <= 0 {
		r = 0 // disable profiling
	} else if rate == 1 {
		r = 1 // profile everything
	} else {
		// convert ns to cycles, use float64 to prevent overflow during multiplication
		r = int64(float64(rate) * float64(tickspersecond()) / (1000 * 1000 * 1000))
		if r == 0 {
			r = 1
		}
	}

	atomic.Store64(&blockprofilerate, uint64(r))
}

func blockevent(cycles int64, skip int) {
	if cycles <= 0 {
		cycles = 1
	}

	rate := int64(atomic.Load64(&blockprofilerate))
	if blocksampled(cycles, rate) {
		saveblockevent(cycles, rate, skip+1, blockProfile)
	}
}

// blocksampled returns true for all events where cycles >= rate. Shorter
// events have a cycles/rate random chance of returning true.
func blocksampled(cycles, rate int64) bool {
	if rate <= 0 || (rate > cycles && cheaprand64()%rate > cycles) {
		return false
	}
	return true
}

// saveblockevent records a profile event of the type specified by which.
// cycles is the quantity associated with this event and rate is the sampling rate,
// used to adjust the cycles value in the manner determined by the profile type.
// skip is the number of frames to omit from the traceback associated with the event.
// The traceback will be recorded from the stack of the goroutine associated with the current m.
// skip should be positive if this event is recorded from the current stack
// (e.g. when this is not called from a system stack)
func saveblockevent(cycles, rate int64, skip int, which bucketType) {
	if debug.profstackdepth == 0 {
		// profstackdepth is set to 0 by the user, so mp.profStack is nil and we
		// can't record a stack trace.
		return
	}
	if skip > maxSkip {
		print("requested skip=", skip)
		throw("invalid skip value")
	}
	gp := getg()
	mp := acquirem() // we must not be preempted while accessing profstack

	var nstk int
	var stk [maxStack]uintptr
	if gp.m.curg == nil || gp.m.curg == gp {
		nstk = callersRaw(stk[:])
	} else {
		// FIXME: This should get a traceback of gp.m.curg.
		// nstk = gcallers(gp.m.curg, skip, stk[:])
		nstk = callersRaw(stk[:])
	}

	saveBlockEventStack(cycles, rate, stk[:nstk], which)
	releasem(mp)
}

// lockTimer assists with profiling contention on runtime-internal locks.
//
// There are several steps between the time that an M experiences contention and
// when that contention may be added to the profile. This comes from our
// constraints: We need to keep the critical section of each lock small,
// especially when those locks are contended. The reporting code cannot acquire
// new locks until the M has released all other locks, which means no memory
// allocations and encourages use of (temporary) M-local storage.
//
// The M will have space for storing one call stack that caused contention, and
// for the magnitude of that contention. It will also have space to store the
// magnitude of additional contention the M caused, since it only has space to
// remember one call stack and might encounter several contention events before
// it releases all of its locks and is thus able to transfer the local buffer
// into the profile.
//
// The M will collect the call stack when it unlocks the contended lock. That
// minimizes the impact on the critical section of the contended lock, and
// matches the mutex profile's behavior for contention in sync.Mutex: measured
// at the Unlock method.
//
// The profile for contention on sync.Mutex blames the caller of Unlock for the
// amount of contention experienced by the callers of Lock which had to wait.
// When there are several critical sections, this allows identifying which of
// them is responsible.
//
// Matching that behavior for runtime-internal locks will require identifying
// which Ms are blocked on the mutex. The semaphore-based implementation is
// ready to allow that, but the futex-based implementation will require a bit
// more work. Until then, we report contention on runtime-internal locks with a
// call stack taken from the unlock call (like the rest of the user-space
// "mutex" profile), but assign it a duration value based on how long the
// previous lock call took (like the user-space "block" profile).
//
// Thus, reporting the call stacks of runtime-internal lock contention is
// guarded by GODEBUG for now. Set GODEBUG=runtimecontentionstacks=1 to enable.
//
// TODO(rhysh): plumb through the delay duration, remove GODEBUG, update comment
//
// The M will track this by storing a pointer to the lock; lock/unlock pairs for
// runtime-internal locks are always on the same M.
//
// Together, that demands several steps for recording contention. First, when
// finally acquiring a contended lock, the M decides whether it should plan to
// profile that event by storing a pointer to the lock in its "to be profiled
// upon unlock" field. If that field is already set, it uses the relative
// magnitudes to weight a random choice between itself and the other lock, with
// the loser's time being added to the "additional contention" field. Otherwise
// if the M's call stack buffer is occupied, it does the comparison against that
// sample's magnitude.
//
// Second, having unlocked a mutex the M checks to see if it should capture the
// call stack into its local buffer. Finally, when the M unlocks its last mutex,
// it transfers the local buffer into the profile. As part of that step, it also
// transfers any "additional contention" time to the profile. Any lock
// contention that it experiences while adding samples to the profile will be
// recorded later as "additional contention" and not include a call stack, to
// avoid an echo.
type lockTimer struct {
	lock      *mutex
	timeRate  int64
	timeStart int64
	tickStart int64
}

func (lt *lockTimer) begin() {
	rate := int64(atomic.Load64(&mutexprofilerate))

	lt.timeRate = gTrackingPeriod
	if rate != 0 && rate < lt.timeRate {
		lt.timeRate = rate
	}
	if int64(cheaprand())%lt.timeRate == 0 {
		lt.timeStart = nanotime()
	}

	if rate > 0 && int64(cheaprand())%rate == 0 {
		lt.tickStart = cputicks()
	}
}

func (lt *lockTimer) end() {
	gp := getg()

	if lt.timeStart != 0 {
		nowTime := nanotime()
		gp.m.mLockProfile.waitTime.Add((nowTime - lt.timeStart) * lt.timeRate)
	}

	if lt.tickStart != 0 {
		nowTick := cputicks()
		gp.m.mLockProfile.recordLock(nowTick-lt.tickStart, lt.lock)
	}
}

type mLockProfile struct {
	waitTime   atomic.Int64 // total nanoseconds spent waiting in runtime.lockWithRank
	stack      []uintptr    // stack that experienced contention in runtime.lockWithRank
	pending    uintptr      // *mutex that experienced contention (to be traceback-ed)
	cycles     int64        // cycles attributable to "pending" (if set), otherwise to "stack"
	cyclesLost int64        // contention for which we weren't able to record a call stack
	disabled   bool         // attribute all time to "lost"
}

func (prof *mLockProfile) recordLock(cycles int64, l *mutex) {
	if cycles <= 0 {
		return
	}

	if prof.disabled {
		// We're experiencing contention while attempting to report contention.
		// Make a note of its magnitude, but don't allow it to be the sole cause
		// of another contention report.
		prof.cyclesLost += cycles
		return
	}

	if uintptr(unsafe.Pointer(l)) == prof.pending {
		// Optimization: we'd already planned to profile this same lock (though
		// possibly from a different unlock site).
		prof.cycles += cycles
		return
	}

	if prev := prof.cycles; prev > 0 {
		// We can only store one call stack for runtime-internal lock contention
		// on this M, and we've already got one. Decide which should stay, and
		// add the other to the report for runtime._LostContendedRuntimeLock.
		prevScore := uint64(cheaprand64()) % uint64(prev)
		thisScore := uint64(cheaprand64()) % uint64(cycles)
		if prevScore > thisScore {
			prof.cyclesLost += cycles
			return
		} else {
			prof.cyclesLost += prev
		}
	}
	// Saving the *mutex as a uintptr is safe because:
	//  - lockrank_on.go does this too, which gives it regular exercise
	//  - the lock would only move if it's stack allocated, which means it
	//      cannot experience multi-M contention
	prof.pending = uintptr(unsafe.Pointer(l))
	prof.cycles = cycles
}

// From unlock2, we might not be holding a p in this code.
//
//go:nowritebarrierrec
func (prof *mLockProfile) recordUnlock(l *mutex) {
	if uintptr(unsafe.Pointer(l)) == prof.pending {
		prof.captureStack()
	}
	if gp := getg(); gp.m.locks == 1 && gp.m.mLockProfile.cycles != 0 {
		prof.store()
	}
}

func (prof *mLockProfile) captureStack() {
	if debug.profstackdepth == 0 {
		// profstackdepth is set to 0 by the user, so mp.profStack is nil and we
		// can't record a stack trace.
		return
	}

	skip := 3 // runtime.(*mLockProfile).recordUnlock runtime.unlock2 runtime.unlockWithRank
	if staticLockRanking {
		// When static lock ranking is enabled, we'll always be on the system
		// stack at this point. There will be a runtime.unlockWithRank.func1
		// frame, and if the call to runtime.unlock took place on a user stack
		// then there'll also be a runtime.systemstack frame. To keep stack
		// traces somewhat consistent whether or not static lock ranking is
		// enabled, we'd like to skip those. But it's hard to tell how long
		// we've been on the system stack so accept an extra frame in that case,
		// with a leaf of "runtime.unlockWithRank runtime.unlock" instead of
		// "runtime.unlock".
		skip += 1 // runtime.unlockWithRank.func1
	}
	_ = skip // callersRaw does its own skip handling
	prof.pending = 0

	prof.stack[0] = logicalStackSentinel
	if debug.runtimeContentionStacks.Load() == 0 {
		prof.stack[1] = abi.FuncPCABIInternal(_LostContendedRuntimeLock) + sys.PCQuantum
		prof.stack[2] = 0
		return
	}

	// gccgo does not have the gc unwinder; use callersRaw like the other
	// profile stack-capture paths in this file.
	var nstk int
	systemstack(func() {
		nstk = callersRaw(prof.stack[:])
	})
	if nstk < len(prof.stack) {
		prof.stack[nstk] = 0
	}
}

func (prof *mLockProfile) store() {
	// Report any contention we experience within this function as "lost"; it's
	// important that the act of reporting a contention event not lead to a
	// reportable contention event. This also means we can use prof.stack
	// without copying, since it won't change during this function.
	mp := acquirem()
	prof.disabled = true

	nstk := int(debug.profstackdepth)
	for i := 0; i < nstk; i++ {
		if pc := prof.stack[i]; pc == 0 {
			nstk = i
			break
		}
	}

	cycles, lost := prof.cycles, prof.cyclesLost
	prof.cycles, prof.cyclesLost = 0, 0

	rate := int64(atomic.Load64(&mutexprofilerate))
	saveBlockEventStack(cycles, rate, prof.stack[:nstk], mutexProfile)
	if lost > 0 {
		lostStk := [...]uintptr{
			logicalStackSentinel,
			abi.FuncPCABIInternal(_LostContendedRuntimeLock) + sys.PCQuantum,
		}
		saveBlockEventStack(lost, rate, lostStk[:], mutexProfile)
	}

	prof.disabled = false
	releasem(mp)
}

func saveBlockEventStack(cycles, rate int64, stk []uintptr, which bucketType) {
	b := stkbucket(which, 0, 0, stk, true)
	bp := b.bp()

	lock(&profBlockLock)
	// We want to up-scale the count and cycles according to the
	// probability that the event was sampled. For block profile events,
	// the sample probability is 1 if cycles >= rate, and cycles / rate
	// otherwise. For mutex profile events, the sample probability is 1 / rate.
	// We scale the events by 1 / (probability the event was sampled).
	if which == blockProfile && cycles < rate {
		// Remove sampling bias, see discussion on http://golang.org/cl/299991.
		bp.count += float64(rate) / float64(cycles)
		bp.cycles += rate
	} else if which == mutexProfile {
		bp.count += float64(rate)
		bp.cycles += rate * cycles
	} else {
		bp.count++
		bp.cycles += cycles
	}
	unlock(&profBlockLock)
}

var mutexprofilerate uint64 // fraction sampled

// SetMutexProfileFraction controls the fraction of mutex contention events
// that are reported in the mutex profile. On average 1/rate events are
// reported. The previous rate is returned.
//
// To turn off profiling entirely, pass rate 0.
// To just read the current rate, pass rate < 0.
// (For n>1 the details of sampling may change.)
func SetMutexProfileFraction(rate int) int {
	if rate < 0 {
		return int(mutexprofilerate)
	}
	old := mutexprofilerate
	atomic.Store64(&mutexprofilerate, uint64(rate))
	return int(old)
}

//go:linkname mutexevent sync.event
func mutexevent(cycles int64, skip int) {
	if cycles < 0 {
		cycles = 0
	}
	rate := int64(atomic.Load64(&mutexprofilerate))
	if rate > 0 && cheaprand64()%rate == 0 {
		saveblockevent(cycles, rate, skip+1, mutexProfile)
	}
}

// Go interface to profile data.

// A StackRecord describes a single execution stack.
type StackRecord struct {
	Stack0 [32]uintptr // stack trace for this record; ends at first 0 entry
}

// Stack returns the stack trace associated with the record,
// a prefix of r.Stack0.
func (r *StackRecord) Stack() []uintptr {
	for i, v := range r.Stack0 {
		if v == 0 {
			return r.Stack0[0:i]
		}
	}
	return r.Stack0[0:]
}

// MemProfileRate controls the fraction of memory allocations
// that are recorded and reported in the memory profile.
// The profiler aims to sample an average of
// one allocation per MemProfileRate bytes allocated.
//
// To include every allocated block in the profile, set MemProfileRate to 1.
// To turn off profiling entirely, set MemProfileRate to 0.
//
// The tools that process the memory profiles assume that the
// profile rate is constant across the lifetime of the program
// and equal to the current value. Programs that change the
// memory profiling rate should do so just once, as early as
// possible in the execution of the program (for example,
// at the beginning of main).
var MemProfileRate int = 512 * 1024

// disableMemoryProfiling is set by the linker if memory profiling
// is not used and the link type guarantees nobody else could use it
// elsewhere.
// We check if the runtime.memProfileInternal symbol is present.
var disableMemoryProfiling bool

// A MemProfileRecord describes the live objects allocated
// by a particular call sequence (stack trace).
type MemProfileRecord struct {
	AllocBytes, FreeBytes     int64       // number of bytes allocated, freed
	AllocObjects, FreeObjects int64       // number of objects allocated, freed
	Stack0                    [32]uintptr // stack trace for this record; ends at first 0 entry
}

// InUseBytes returns the number of bytes in use (AllocBytes - FreeBytes).
func (r *MemProfileRecord) InUseBytes() int64 { return r.AllocBytes - r.FreeBytes }

// InUseObjects returns the number of objects in use (AllocObjects - FreeObjects).
func (r *MemProfileRecord) InUseObjects() int64 {
	return r.AllocObjects - r.FreeObjects
}

// Stack returns the stack trace associated with the record,
// a prefix of r.Stack0.
func (r *MemProfileRecord) Stack() []uintptr {
	for i, v := range r.Stack0 {
		if v == 0 {
			return r.Stack0[0:i]
		}
	}
	return r.Stack0[0:]
}

// reusebucket tries to pick a prunedProfile bucket off
// the freebuckets list, returning it if one is available or nil
// if the free list is empty.
func reusebucket(nstk int) *bucket {
	var b *bucket
	if freebuckets != nil {
		b = freebuckets
		freebuckets = freebuckets.allnext
		b.typ = prunedProfile
		b.nstk = uintptr(nstk)
		mp := b.mp()
		// Hack: rely on the fact that memprofile records are
		// larger than blockprofile records when clearing.
		*mp = memRecord{}
	}
	return b
}

// freebucket appends the specified prunedProfile bucket
// onto the free list, and removes references to it from the hash.
func freebucket(tofree *bucket) *bucket {
	// Thread this bucket into the free list.
	ret := tofree.allnext
	tofree.allnext = freebuckets
	freebuckets = tofree

	// Clean up the hash. The hash may point directly to this bucket...
	bh := (*buckhashArray)(buckhash.Load())
	i := int(tofree.hash % buckHashSize)
	if (*bucket)(bh[i].Load()) == tofree {
		bh[i].StoreNoWB(unsafe.Pointer(tofree.next))
	} else {
		// ... or when this bucket was inserted by stkbucket, it may have been
		// chained off some other unrelated bucket.
		for b := (*bucket)(bh[i].Load()); b != nil; b = b.next {
			if b.next == tofree {
				b.next = tofree.next
				break
			}
		}
	}
	return ret
}

// fixupStack takes a 'raw' stack trace (stack of PCs generated by
// callersRaw) and performs pre-symbolization fixup on it, returning
// the results in 'canonStack'. For each frame we look at the
// file/func/line information, then use that info to decide whether to
// include the frame in the final symbolized stack (removing frames
// corresponding to 'morestack' routines, for example). We also expand
// frames if the PC values to which they refer correponds to inlined
// functions to allow for expanded symbolic info to be filled in
// later. Note: there is code in go-callers.c's backtrace_full callback()
// function that performs very similar fixups; these two code paths
// should be kept in sync.
func fixupStack(stk []uintptr, skip int, canonStack *[maxStack]uintptr, size uintptr) int {
	var cidx int
	var termTrace bool
	// Increase the skip count to take into account the frames corresponding
	// to runtime.callersRaw and to the C routine that it invokes.
	skip += 2
	sawSigtramp := false
	for _, pc := range stk {
		// Subtract 1 from PC to undo the 1 we added in callback in
		// go-callers.c.
		function, file, _, frames := funcfileline(pc-1, -1, false)

		// Skip an unnamed function above sigtramp, as it is
		// likely the signal handler.
		if sawSigtramp {
			sawSigtramp = false
			if function == "" {
				continue
			}
		}

		// Skip split-stack functions (match by function name)
		skipFrame := false
		if hasPrefix(function, "_____morestack_") || hasPrefix(function, "__morestack_") {
			skipFrame = true
		}

		// Skip split-stack functions (match by file)
		if hasSuffix(file, "/morestack.S") {
			skipFrame = true
		}

		// Skip thunks and recover functions and other functions
		// specific to gccgo, that do not appear in the gc toolchain.
		fcn := function
		if hasSuffix(fcn, "..r") {
			skipFrame = true
		} else if function == "runtime.deferreturn" || function == "runtime.sighandler" {
			skipFrame = true
		} else if function == "runtime.sigtramp" || function == "runtime.sigtrampgo" {
			skipFrame = true
			// Also skip subsequent unnamed functions,
			// which will be the signal handler itself.
			sawSigtramp = true
		} else {
			for fcn != "" && (fcn[len(fcn)-1] >= '0' && fcn[len(fcn)-1] <= '9') {
				fcn = fcn[:len(fcn)-1]
			}
			if hasSuffix(fcn, "..stub") || hasSuffix(fcn, "..thunk") {
				skipFrame = true
			}
		}
		if skipFrame {
			continue
		}

		// Terminate the trace if we encounter a frame corresponding to
		// runtime.main, runtime.kickoff, makecontext, etc. See the
		// corresponding code in go-callers.c, callback function used
		// with backtrace_full.
		if function == "makecontext" {
			termTrace = true
		}
		if hasSuffix(file, "/proc.c") && function == "runtime_mstart" {
			termTrace = true
		}
		if hasSuffix(file, "/proc.go") &&
			(function == "runtime.main" || function == "runtime.kickoff") {
			termTrace = true
		}

		// Expand inline frames.
		for i := 0; i < frames; i++ {
			(*canonStack)[cidx] = pc
			cidx++
			if cidx >= maxStack {
				termTrace = true
				break
			}
		}
		if termTrace {
			break
		}
	}

	// Apply skip count. Needs to be done after expanding inline frames.
	if skip != 0 {
		if skip >= cidx {
			return 0
		}
		copy(canonStack[:cidx-skip], canonStack[skip:])
		return cidx - skip
	}

	return cidx
}

// fixupBucket takes a raw memprofile bucket and creates a new bucket
// in which the stack trace has been fixed up (inline frames expanded,
// unwanted frames stripped out). Original bucket is left unmodified;
// a new symbolizeProfile bucket may be generated as a side effect.
// Payload information from the original bucket is incorporated into
// the new bucket.
func fixupBucket(b *bucket) {
	var canonStack [maxStack]uintptr
	frames := fixupStack(b.stk(), b.skip, &canonStack, b.size)
	cb := stkbucket(prunedProfile, b.size, 0, canonStack[:frames], true)
	switch b.typ {
	default:
		throw("invalid profile bucket type")
	case memProfile:
		rawrecord := b.mp()
		cb.mp().active.add(&rawrecord.active)
	case blockProfile, mutexProfile:
		cb.bp().count += b.bp().count
		cb.bp().cycles += b.bp().cycles
	}
}

// MemProfile returns a profile of memory allocated and freed per allocation
// site.
//
// MemProfile returns n, the number of records in the current memory profile.
// If len(p) >= n, MemProfile copies the profile into p and returns n, true.
// If len(p) < n, MemProfile does not change p and returns n, false.
//
// If inuseZero is true, the profile includes allocation records
// where r.AllocBytes > 0 but r.AllocBytes == r.FreeBytes.
// These are sites where memory was allocated, but it has all
// been released back to the runtime.
//
// The returned profile may be up to two garbage collection cycles old.
// This is to avoid skewing the profile toward allocations; because
// allocations happen in real time but frees are delayed until the garbage
// collector performs sweeping, the profile only accounts for allocations
// that have had a chance to be freed by the garbage collector.
//
// Most clients should use the runtime/pprof package or
// the testing package's -test.memprofile flag instead
// of calling MemProfile directly.
func MemProfile(p []MemProfileRecord, inuseZero bool) (n int, ok bool) {
	return memProfileInternal(len(p), inuseZero, func(r profilerecord.MemProfileRecord) {
		copyMemProfileRecord(&p[0], r)
		p = p[1:]
	})
}

// memProfileInternal returns the number of records n in the profile. If there
// are less than size records, copyFn is invoked for each record, and ok returns
// true.
//
// The linker set disableMemoryProfiling to true to disable memory profiling
// if this function is not reachable. Mark it noinline to ensure the symbol exists.
// (This function is big and normally not inlined anyway.)
// See also disableMemoryProfiling above and cmd/link/internal/ld/lib.go:linksetup.
//
//go:noinline
func memProfileInternal(size int, inuseZero bool, copyFn func(profilerecord.MemProfileRecord)) (n int, ok bool) {
	cycle := mProfCycle.read()
	// If we're between mProf_NextCycle and mProf_Flush, take care
	// of flushing to the active profile so we only have to look
	// at the active profile below.
	index := cycle % uint32(len(memRecord{}.future))
	lock(&profMemActiveLock)
	lock(&profMemFutureLock[index])
	mProf_FlushLocked(index)
	unlock(&profMemFutureLock[index])
	clear := true
	head := (*bucket)(mbuckets.Load())
	for b := head; b != nil; b = b.allnext {
		mp := b.mp()
		if inuseZero || mp.active.alloc_bytes != mp.active.free_bytes {
			n++
		}
		if mp.active.allocs != 0 || mp.active.frees != 0 {
			clear = false
		}
	}
	if clear {
		// Absolutely no data, suggesting that a garbage collection
		// has not yet happened. In order to allow profiling when
		// garbage collection is disabled from the beginning of execution,
		// accumulate all of the cycles, and recount buckets.
		n = 0
		for b := head; b != nil; b = b.allnext {
			mp := b.mp()
			for c := range mp.future {
				lock(&profMemFutureLock[c])
				mp.active.add(&mp.future[c])
				mp.future[c] = memRecordCycle{}
				unlock(&profMemFutureLock[c])
			}
			if inuseZero || mp.active.alloc_bytes != mp.active.free_bytes {
				n++
			}
		}
	}
	if n <= size {
		ok = true
		var bnext *bucket

		// Post-process raw buckets to fix up their stack traces (gccgo records
		// raw PCs and defers symbolization/pruning to here).
		for b := head; b != nil; b = b.allnext {
			mp := b.mp()
			if inuseZero || mp.active.alloc_bytes != mp.active.free_bytes {
				fixupBucket(b)
			}
		}

		// Report pruned/fixed-up buckets.
		idx := 0
		for b := sbuckets; b != nil; b = b.allnext {
			bmp := b.mp()
			r := profilerecord.MemProfileRecord{
				AllocBytes:   int64(bmp.active.alloc_bytes),
				FreeBytes:    int64(bmp.active.free_bytes),
				AllocObjects: int64(bmp.active.allocs),
				FreeObjects:  int64(bmp.active.frees),
				Stack:        b.stk(),
			}
			copyFn(r)
			idx++
		}
		n = idx

		// Free up pruned buckets for use in next round.
		for b := sbuckets; b != nil; b = bnext {
			bnext = freebucket(b)
		}
		sbuckets = nil
	}
	unlock(&profMemActiveLock)
	return
}

func copyMemProfileRecord(dst *MemProfileRecord, src profilerecord.MemProfileRecord) {
	dst.AllocBytes = src.AllocBytes
	dst.FreeBytes = src.FreeBytes
	dst.AllocObjects = src.AllocObjects
	dst.FreeObjects = src.FreeObjects
	if raceenabled {
		racewriterangepc(unsafe.Pointer(&dst.Stack0[0]), unsafe.Sizeof(dst.Stack0), getcallerpc(), abi.FuncPCABIInternal(MemProfile))
	}
	if msanenabled {
		msanwrite(unsafe.Pointer(&dst.Stack0[0]), unsafe.Sizeof(dst.Stack0))
	}
	if asanenabled {
		asanwrite(unsafe.Pointer(&dst.Stack0[0]), unsafe.Sizeof(dst.Stack0))
	}
	i := copy(dst.Stack0[:], src.Stack)
	clear(dst.Stack0[i:])
}

//go:linkname pprof_memProfileInternal runtime.pprof_memProfileInternal
func pprof_memProfileInternal(p []profilerecord.MemProfileRecord, inuseZero bool) (n int, ok bool) {
	return memProfileInternal(len(p), inuseZero, func(r profilerecord.MemProfileRecord) {
		p[0] = r
		p = p[1:]
	})
}

func iterate_memprof(fn func(*bucket, uintptr, *uintptr, uintptr, uintptr, uintptr)) {
	lock(&profMemActiveLock)
	head := (*bucket)(mbuckets.Load())
	for b := head; b != nil; b = b.allnext {
		mp := b.mp()
		fn(b, b.nstk, &b.stk()[0], b.size, mp.active.allocs, mp.active.frees)
	}
	unlock(&profMemActiveLock)
}

// BlockProfileRecord describes blocking events originated
// at a particular call sequence (stack trace).
type BlockProfileRecord struct {
	Count  int64
	Cycles int64
	StackRecord
}

// BlockProfile returns n, the number of records in the current blocking profile.
// If len(p) >= n, BlockProfile copies the profile into p and returns n, true.
// If len(p) < n, BlockProfile does not change p and returns n, false.
//
// Most clients should use the [runtime/pprof] package or
// the [testing] package's -test.blockprofile flag instead
// of calling BlockProfile directly.
func BlockProfile(p []BlockProfileRecord) (n int, ok bool) {
	var m int
	n, ok = blockProfileInternal(len(p), func(r profilerecord.BlockProfileRecord) {
		copyBlockProfileRecord(&p[m], r)
		m++
	})
	return
}

// blockProfileInternal returns the number of records n in the profile. If there
// are less than size records, copyFn is invoked for each record, and ok returns
// true.
func blockProfileInternal(size int, copyFn func(profilerecord.BlockProfileRecord)) (n int, ok bool) {
	lock(&profBlockLock)
	n, ok = harvestBlockMutexProfile(size, (*bucket)(bbuckets.Load()), copyFn)
	unlock(&profBlockLock)
	return
}

// harvestBlockMutexProfile counts and, if there is room, reports the buckets in
// the given raw block/mutex profile list. gccgo records raw PCs and defers
// symbolization/pruning until here, so the raw buckets are fixed up into pruned
// buckets (sbuckets) before being reported. The caller must hold profBlockLock.
func harvestBlockMutexProfile(size int, buckets *bucket, copyFn func(profilerecord.BlockProfileRecord)) (n int, ok bool) {
	for b := buckets; b != nil; b = b.allnext {
		n++
	}
	if n <= size {
		var bnext *bucket

		// Post-process raw buckets to create pruned/fixed-up buckets.
		for b := buckets; b != nil; b = bnext {
			bnext = b.allnext
			fixupBucket(b)
		}

		ok = true
		for b := sbuckets; b != nil; b = b.allnext {
			bp := b.bp()
			r := profilerecord.BlockProfileRecord{
				Count:  int64(bp.count),
				Cycles: bp.cycles,
				Stack:  b.stk(),
			}
			// Prevent callers from having to worry about division by zero errors.
			// See discussion on http://golang.org/cl/299991.
			if r.Count == 0 {
				r.Count = 1
			}
			copyFn(r)
		}

		// Free up pruned buckets for use in next round.
		for b := sbuckets; b != nil; b = bnext {
			bnext = freebucket(b)
		}
		sbuckets = nil
	}
	return
}

// copyBlockProfileRecord copies the sample values and call stack from src to dst.
// The call stack is copied as-is. The caller is responsible for handling inline
// expansion, needed when the call stack was collected with frame pointer unwinding.
func copyBlockProfileRecord(dst *BlockProfileRecord, src profilerecord.BlockProfileRecord) {
	dst.Count = src.Count
	dst.Cycles = src.Cycles
	if raceenabled {
		racewriterangepc(unsafe.Pointer(&dst.Stack0[0]), unsafe.Sizeof(dst.Stack0), getcallerpc(), abi.FuncPCABIInternal(BlockProfile))
	}
	if msanenabled {
		msanwrite(unsafe.Pointer(&dst.Stack0[0]), unsafe.Sizeof(dst.Stack0))
	}
	if asanenabled {
		asanwrite(unsafe.Pointer(&dst.Stack0[0]), unsafe.Sizeof(dst.Stack0))
	}
	// We just copy the stack here without inline expansion
	// (needed if frame pointer unwinding is used)
	// since this function is called under the profile lock,
	// and doing something that might allocate can violate lock ordering.
	i := copy(dst.Stack0[:], src.Stack)
	clear(dst.Stack0[i:])
}

//go:linkname pprof_blockProfileInternal runtime.pprof_blockProfileInternal
func pprof_blockProfileInternal(p []profilerecord.BlockProfileRecord) (n int, ok bool) {
	return blockProfileInternal(len(p), func(r profilerecord.BlockProfileRecord) {
		p[0] = r
		p = p[1:]
	})
}

// MutexProfile returns n, the number of records in the current mutex profile.
// If len(p) >= n, MutexProfile copies the profile into p and returns n, true.
// Otherwise, MutexProfile does not change p, and returns n, false.
//
// Most clients should use the [runtime/pprof] package
// instead of calling MutexProfile directly.
func MutexProfile(p []BlockProfileRecord) (n int, ok bool) {
	var m int
	n, ok = mutexProfileInternal(len(p), func(r profilerecord.BlockProfileRecord) {
		copyBlockProfileRecord(&p[m], r)
		m++
	})
	return
}

// mutexProfileInternal returns the number of records n in the profile. If there
// are less than size records, copyFn is invoked for each record, and ok returns
// true.
func mutexProfileInternal(size int, copyFn func(profilerecord.BlockProfileRecord)) (n int, ok bool) {
	lock(&profBlockLock)
	n, ok = harvestBlockMutexProfile(size, (*bucket)(xbuckets.Load()), copyFn)
	unlock(&profBlockLock)
	return
}

//go:linkname pprof_mutexProfileInternal runtime.pprof_mutexProfileInternal
func pprof_mutexProfileInternal(p []profilerecord.BlockProfileRecord) (n int, ok bool) {
	return mutexProfileInternal(len(p), func(r profilerecord.BlockProfileRecord) {
		p[0] = r
		p = p[1:]
	})
}

// ThreadCreateProfile returns n, the number of records in the thread creation profile.
// If len(p) >= n, ThreadCreateProfile copies the profile into p and returns n, true.
// If len(p) < n, ThreadCreateProfile does not change p and returns n, false.
//
// Most clients should use the runtime/pprof package instead
// of calling ThreadCreateProfile directly.
func ThreadCreateProfile(p []StackRecord) (n int, ok bool) {
	return threadCreateProfileInternal(len(p), func(r profilerecord.StackRecord) {
		copy(p[0].Stack0[:], r.Stack)
		p = p[1:]
	})
}

// threadCreateStackScratch is a scratch buffer used by
// threadCreateProfileInternal to flatten an m's createstack into a slice of
// PCs without allocating (and without letting a stack-local array escape).
// It is only used while walking allm in threadCreateProfileInternal.
var threadCreateStackScratch [32]uintptr // must match m.createstack length

// threadCreateProfileInternal returns the number of records n in the profile.
// If there are less than size records, copyFn is invoked for each record, and
// ok returns true.
func threadCreateProfileInternal(size int, copyFn func(profilerecord.StackRecord)) (n int, ok bool) {
	first := (*m)(atomic.Loadp(unsafe.Pointer(&allm)))
	for mp := first; mp != nil; mp = mp.alllink {
		n++
	}
	if n <= size {
		ok = true
		for mp := first; mp != nil; mp = mp.alllink {
			// gccgo stores createstack as []location; flatten to the PCs
			// that profilerecord.StackRecord expects. Use a package-level
			// scratch buffer rather than a stack-local array: slicing a
			// stack-local array into the record and passing it to copyFn
			// would make the array escape, which is not allowed in the
			// runtime. copyFn copies the stack out before the next
			// iteration reuses the buffer.
			nstk := 0
			for j := range mp.createstack {
				pc := mp.createstack[j].pc
				if pc == 0 {
					break
				}
				threadCreateStackScratch[j] = pc
				nstk++
			}
			r := profilerecord.StackRecord{Stack: threadCreateStackScratch[:nstk]}
			copyFn(r)
		}
	}
	return
}

//go:linkname pprof_threadCreateInternal runtime.pprof_threadCreateInternal
func pprof_threadCreateInternal(p []profilerecord.StackRecord) (n int, ok bool) {
	return threadCreateProfileInternal(len(p), func(r profilerecord.StackRecord) {
		p[0] = r
		p = p[1:]
	})
}

//go:linkname pprof_goroutineProfileWithLabels runtime.pprof_goroutineProfileWithLabels
func pprof_goroutineProfileWithLabels(p []profilerecord.StackRecord, labels []unsafe.Pointer) (n int, ok bool) {
	return goroutineProfileWithLabels(p, labels)
}

// labels may be nil. If labels is non-nil, it must have the same length as p.
func goroutineProfileWithLabels(p []profilerecord.StackRecord, labels []unsafe.Pointer) (n int, ok bool) {
	if labels != nil && len(labels) != len(p) {
		labels = nil
	}

	return goroutineProfileWithLabelsConcurrent(p, labels)
}

//go:linkname pprof_goroutineLeakProfileWithLabels
func pprof_goroutineLeakProfileWithLabels(p []profilerecord.StackRecord, labels []unsafe.Pointer) (n int, ok bool) {
	return goroutineLeakProfileWithLabels(p, labels)
}

// labels may be nil. If labels is non-nil, it must have the same length as p.
func goroutineLeakProfileWithLabels(p []profilerecord.StackRecord, labels []unsafe.Pointer) (n int, ok bool) {
	if labels != nil && len(labels) != len(p) {
		labels = nil
	}

	return goroutineLeakProfileWithLabelsConcurrent(p, labels)
}

var goroutineProfile = struct {
	sema    uint32
	active  bool
	offset  atomic.Int64
	records []profilerecord.StackRecord
	labels  []unsafe.Pointer
}{
	sema: 1,
}

// goroutineProfileState indicates the status of a goroutine's stack for the
// current in-progress goroutine profile. Goroutines' stacks are initially
// "Absent" from the profile, and end up "Satisfied" by the time the profile is
// complete. While a goroutine's stack is being captured, its
// goroutineProfileState will be "InProgress" and it will not be able to run
// until the capture completes and the state moves to "Satisfied".
//
// Some goroutines (the finalizer goroutine, which at various times can be
// either a "system" or a "user" goroutine, and the goroutine that is
// coordinating the profile, any goroutines created during the profile) move
// directly to the "Satisfied" state.
type goroutineProfileState uint32

const (
	goroutineProfileAbsent goroutineProfileState = iota
	goroutineProfileInProgress
	goroutineProfileSatisfied
)

type goroutineProfileStateHolder atomic.Uint32

func (p *goroutineProfileStateHolder) Load() goroutineProfileState {
	return goroutineProfileState((*atomic.Uint32)(p).Load())
}

func (p *goroutineProfileStateHolder) Store(value goroutineProfileState) {
	(*atomic.Uint32)(p).Store(uint32(value))
}

func (p *goroutineProfileStateHolder) CompareAndSwap(old, new goroutineProfileState) bool {
	return (*atomic.Uint32)(p).CompareAndSwap(uint32(old), uint32(new))
}

func goroutineLeakProfileWithLabelsConcurrent(p []profilerecord.StackRecord, labels []unsafe.Pointer) (n int, ok bool) {
	if len(p) == 0 {
		// An empty slice is obviously too small. Return a rough
		// allocation estimate.
		return work.goroutineLeak.count, false
	}

	pcbuf := makeProfStack() // see saveg() for explanation

	// Prepare a profile large enough to store all leaked goroutines.
	n = work.goroutineLeak.count

	if n > len(p) {
		// There's not enough space in p to store the whole profile, so
		// we're not allowed to write to p at all and must return n, false.
		return n, false
	}

	// Visit each leaked goroutine and try to record its stack.
	var offset int
	forEachGRace(func(gp1 *g) {
		if readgstatus(gp1)&^_Gscan == _Gleaked {
			systemstack(func() { saveg(^uintptr(0), ^uintptr(0), gp1, &p[offset], pcbuf) })
			if labels != nil {
				labels[offset] = gp1.labels
			}
			offset++
		}
	})

	if raceenabled {
		raceacquire(unsafe.Pointer(&labelSync))
	}

	return n, true
}

func goroutineProfileWithLabelsConcurrent(p []profilerecord.StackRecord, labels []unsafe.Pointer) (n int, ok bool) {
	if len(p) == 0 {
		// An empty slice is obviously too small. Return a rough
		// allocation estimate without bothering to STW. As long as
		// this is close, then we'll only need to STW once (on the next
		// call).
		return int(gcount(false)), false
	}

	semacquire(&goroutineProfile.sema)

	ourg := getg()

	pcbuf := makeProfStack() // see saveg() for explanation
	stw := stopTheWorld(stwGoroutineProfile)
	// Using gcount while the world is stopped should give us a consistent view
	// of the number of live goroutines, minus the number of goroutines that are
	// alive and permanently marked as "system". But to make this count agree
	// with what we'd get from isSystemGoroutine, we need special handling for
	// goroutines that can vary between user and system to ensure that the count
	// doesn't change during the collection. So, check the finalizer goroutine
	// and cleanup goroutines in particular.
	n = int(gcount())
	// gccgo tracks the finalizer goroutine's system-ness via
	// fing.isSystemGoroutine (toggled off while it runs a finalizer, see
	// mfinal.go) rather than gc's fingStatus/fingRunningFinalizer bits.
	if fing != nil && !fing.isSystemGoroutine {
		n++
	}
	// gccgo does not implement Go 1.25 cleanups, so there are no
	// additional running cleanup goroutines to account for.

	if n > len(p) {
		// There's not enough space in p to store the whole profile, so (per the
		// contract of runtime.GoroutineProfile) we're not allowed to write to p
		// at all and must return n, false.
		startTheWorld(stw)
		semrelease(&goroutineProfile.sema)
		return n, false
	}

	// Save current goroutine.
	systemstack(func() {
		saveg(ourg, &p[0], pcbuf)
	})
	if labels != nil {
		labels[0] = ourg.labels
	}
	ourg.goroutineProfiled.Store(goroutineProfileSatisfied)
	goroutineProfile.offset.Store(1)

	// Prepare for all other goroutines to enter the profile. Aside from ourg,
	// every goroutine struct in the allgs list has its goroutineProfiled field
	// cleared. Any goroutine created from this point on (while
	// goroutineProfile.active is set) will start with its goroutineProfiled
	// field set to goroutineProfileSatisfied.
	goroutineProfile.active = true
	goroutineProfile.records = p
	goroutineProfile.labels = labels
	startTheWorld(stw)

	// Visit each goroutine that existed as of the startTheWorld call above.
	//
	// New goroutines may not be in this list, but we didn't want to know about
	// them anyway. If they do appear in this list (via reusing a dead goroutine
	// struct, or racing to launch between the world restarting and us getting
	// the list), they will already have their goroutineProfiled field set to
	// goroutineProfileSatisfied before their state transitions out of _Gdead.
	//
	// Any goroutine that the scheduler tries to execute concurrently with this
	// call will start by adding itself to the profile (before the act of
	// executing can cause any changes in its stack).
	forEachGRace(func(gp1 *g) {
		tryRecordGoroutineProfile(gp1, pcbuf, Gosched)
	})

	stw = stopTheWorld(stwGoroutineProfileCleanup)
	endOffset := goroutineProfile.offset.Swap(0)
	goroutineProfile.active = false
	goroutineProfile.records = nil
	goroutineProfile.labels = nil
	startTheWorld(stw)

	// Restore the invariant that every goroutine struct in allgs has its
	// goroutineProfiled field cleared.
	forEachGRace(func(gp1 *g) {
		gp1.goroutineProfiled.Store(goroutineProfileAbsent)
	})

	if raceenabled {
		raceacquire(unsafe.Pointer(&labelSync))
	}

	if n != int(endOffset) {
		// It's a big surprise that the number of goroutines changed while we
		// were collecting the profile. But probably better to return a
		// truncated profile than to crash the whole process.
		//
		// For instance, needm moves a goroutine out of the _Gdeadextra state and so
		// might be able to change the goroutine count without interacting with
		// the scheduler. For code like that, the race windows are small and the
		// combination of features is uncommon, so it's hard to be (and remain)
		// sure we've caught them all.
	}

	semrelease(&goroutineProfile.sema)
	return n, true
}

// tryRecordGoroutineProfileWB asserts that write barriers are allowed and calls
// tryRecordGoroutineProfile.
//
//go:yeswritebarrierrec
func tryRecordGoroutineProfileWB(gp1 *g) {
	if getg().m.p.ptr() == nil {
		throw("no P available, write barriers are forbidden")
	}
	tryRecordGoroutineProfile(gp1, nil, osyield)
}

// tryRecordGoroutineProfile ensures that gp1 has the appropriate representation
// in the current goroutine profile: either that it should not be profiled, or
// that a snapshot of its call stack and labels are now in the profile.
func tryRecordGoroutineProfile(gp1 *g, pcbuf []uintptr, yield func()) {
	if status := readgstatus(gp1); status == _Gdead || status == _Gdeadextra {
		// Dead goroutines should not appear in the profile. Goroutines that
		// start while profile collection is active will get goroutineProfiled
		// set to goroutineProfileSatisfied before transitioning out of _Gdead,
		// so here we check _Gdead first.
		return
	}

	for {
		prev := gp1.goroutineProfiled.Load()
		if prev == goroutineProfileSatisfied {
			// This goroutine is already in the profile (or is new since the
			// start of collection, so shouldn't appear in the profile).
			break
		}
		if prev == goroutineProfileInProgress {
			// Something else is adding gp1 to the goroutine profile right now.
			// Give that a moment to finish.
			yield()
			continue
		}

		// While we have gp1.goroutineProfiled set to
		// goroutineProfileInProgress, gp1 may appear _Grunnable but will not
		// actually be able to run. Disable preemption for ourselves, to make
		// sure we finish profiling gp1 right away instead of leaving it stuck
		// in this limbo.
		mp := acquirem()
		if gp1.goroutineProfiled.CompareAndSwap(goroutineProfileAbsent, goroutineProfileInProgress) {
			doRecordGoroutineProfile(gp1, pcbuf)
			gp1.goroutineProfiled.Store(goroutineProfileSatisfied)
		}
		releasem(mp)
	}
}

// doRecordGoroutineProfile writes gp1's call stack and labels to an in-progress
// goroutine profile. Preemption is disabled.
//
// This may be called via tryRecordGoroutineProfile in two ways: by the
// goroutine that is coordinating the goroutine profile (running on its own
// stack), or from the scheduler in preparation to execute gp1 (running on the
// system stack).
func doRecordGoroutineProfile(gp1 *g, pcbuf []uintptr) {
	if isSystemGoroutine(gp1, false) {
		// System goroutines should not appear in the profile.
		// Check this here and not in tryRecordGoroutineProfile because isSystemGoroutine
		// may change on a goroutine while it is executing, so while the scheduler might
		// see a system goroutine, goroutineProfileWithLabelsConcurrent might not, and
		// this inconsistency could cause invariants to be violated, such as trying to
		// record the stack of a running goroutine below. In short, we still want system
		// goroutines to participate in the same state machine on gp1.goroutineProfiled as
		// everything else, we just don't record the stack in the profile.
		return
	}
	// Double-check that we didn't make a grave mistake. If the G is running then in
	// general, we cannot safely read its stack.
	//
	// However, there is one case where it's OK. There's a small window of time in
	// exitsyscall where a goroutine could be in _Grunning as it's exiting a syscall.
	// This is OK because goroutine will not exit the syscall until it passes through
	// a call to tryRecordGoroutineProfile. (An explicit one on the fast path, an
	// implicit one via the scheduler on the slow path.)
	//
	// This is also why it's safe to check syscallsp here. The syscall path mutates
	// syscallsp only after passing through tryRecordGoroutineProfile.
	if readgstatus(gp1) == _Grunning && gp1.syscallsp == 0 {
		print("doRecordGoroutineProfile gp1=", gp1.goid, "\n")
		throw("cannot read stack of running goroutine")
	}

	offset := int(goroutineProfile.offset.Add(1)) - 1

	if offset >= len(goroutineProfile.records) {
		// Should be impossible, but better to return a truncated profile than
		// to crash the entire process at this point. Instead, deal with it in
		// goroutineProfileWithLabelsConcurrent where we have more context.
		return
	}

	// saveg calls gentraceback, which may call cgo traceback functions. When
	// called from the scheduler, this is on the system stack already so
	// traceback.go:cgoContextPCs will avoid calling back into the scheduler.
	//
	// When called from the goroutine coordinating the profile, we still have
	// set gp1.goroutineProfiled to goroutineProfileInProgress and so are still
	// preventing it from being truly _Grunnable. So we'll use the system stack
	// to avoid schedule delays.
	systemstack(func() { saveg(gp1, &goroutineProfile.records[offset], pcbuf) })

	if goroutineProfile.labels != nil {
		goroutineProfile.labels[offset] = gp1.labels
	}
}

func goroutineProfileWithLabelsSync(p []profilerecord.StackRecord, labels []unsafe.Pointer) (n int, ok bool) {
	gp := getg()

	isOK := func(gp1 *g) bool {
		// Checking isSystemGoroutine here makes GoroutineProfile
		// consistent with both NumGoroutine and Stack.
		if gp1 == gp {
			return false
		}
		if status := readgstatus(gp1); status == _Gdead || status == _Gdeadextra {
			return false
		}
		if isSystemGoroutine(gp1, false) {
			return false
		}
		return true
	}

	pcbuf := makeProfStack() // see saveg() for explanation
	stw := stopTheWorld(stwGoroutineProfile)

	// World is stopped, no locking required.
	n = 1
	forEachGRace(func(gp1 *g) {
		if isOK(gp1) {
			n++
		}
	})

	if n <= len(p) {
		ok = true
		r, lbl := p, labels

		// Save current goroutine.
		systemstack(func() {
			saveg(gp, &r[0], pcbuf)
		})
		r = r[1:]

		// If we have a place to put our goroutine labelmap, insert it there.
		if labels != nil {
			lbl[0] = gp.labels
			lbl = lbl[1:]
		}

		// Save other goroutines.
		forEachGRace(func(gp1 *g) {
			if isOK(gp1) {
				return
			}

			if len(r) == 0 {
				// Should be impossible, but better to return a
				// truncated profile than to crash the entire process.
				return
			}
			// saveg calls the traceback machinery, which may call cgo traceback
			// functions. The world is stopped, so it cannot use cgocall (which
			// will be blocked at exitsyscall). Do it on the system stack so it
			// won't call into the scheduler (see traceback.go:cgoContextPCs).
			systemstack(func() { saveg(gp1, &r[0], pcbuf) })
			if labels != nil {
				lbl[0] = gp1.labels
				lbl = lbl[1:]
			}
			r = r[1:]
		})
	}

	if raceenabled {
		raceacquire(unsafe.Pointer(&labelSync))
	}

	startTheWorld(stw)
	return n, ok
}

// GoroutineProfile returns n, the number of records in the active goroutine stack profile.
// If len(p) >= n, GoroutineProfile copies the profile into p and returns n, true.
// If len(p) < n, GoroutineProfile does not change p and returns n, false.
//
// Most clients should use the [runtime/pprof] package instead
// of calling GoroutineProfile directly.
func GoroutineProfile(p []StackRecord) (n int, ok bool) {
	records := make([]profilerecord.StackRecord, len(p))
	n, ok = goroutineProfileInternal(records)
	if !ok {
		return
	}
	for i, mr := range records[0:n] {
		copy(p[i].Stack0[:], mr.Stack)
	}
	return
}

func goroutineProfileInternal(p []profilerecord.StackRecord) (n int, ok bool) {
	return goroutineProfileWithLabels(p, nil)
}

func saveg(gp *g, r *profilerecord.StackRecord, pcbuf []uintptr) {
	// gccgo uses its own traceback machinery (callers/callersRaw) rather than
	// the gc frame-pointer unwinder, and it can currently only traceback the
	// current goroutine. To reduce memory usage we record into the temporary
	// pcbuf (allocating one if the caller did not provide it) and then allocate
	// r.Stack to just the size needed.
	if pcbuf == nil {
		pcbuf = makeProfStack()
	}

	if gp == getg() {
		var locbuf [maxStack]location
		m := callers(1, locbuf[:])
		n := 0
		for ; n < m && n < len(pcbuf); n++ {
			pcbuf[n] = locbuf[n].pc
		}
		r.Stack = make([]uintptr, n)
		copy(r.Stack, pcbuf[:n])
	} else {
		// FIXME: Not implemented for other goroutines; gccgo lacks a
		// cross-goroutine unwinder here.
		r.Stack = nil
	}
}

// Stack formats a stack trace of the calling goroutine into buf
// and returns the number of bytes written to buf.
// If all is true, Stack formats stack traces of all other goroutines
// into buf after the trace for the current goroutine.
func Stack(buf []byte, all bool) int {
	var stw worldStop
	if all {
		stw = stopTheWorld(stwAllGoroutinesStack)
	}

	n := 0
	if len(buf) > 0 {
		gp := getg()
		// Force traceback=1 to override GOTRACEBACK setting,
		// so that Stack's results are consistent.
		// GOTRACEBACK is only about crash dumps.
		gp.m.traceback = 1
		gp.writebuf = buf[0:0:len(buf)]
		goroutineheader(gp)
		traceback(1)
		if all {
			tracebackothers(gp)
		}
		gp.m.traceback = 0
		n = len(gp.writebuf)
		gp.writebuf = nil
	}

	if all {
		startTheWorld(stw)
	}
	return n
}

// Tracing of alloc/free/gc.

var tracelock mutex

func tracealloc(p unsafe.Pointer, size uintptr, typ *_type) {
	lock(&tracelock)
	gp := getg()
	gp.m.traceback = 2
	if typ == nil {
		print("tracealloc(", p, ", ", hex(size), ")\n")
	} else {
		print("tracealloc(", p, ", ", hex(size), ", ", typ.string(), ")\n")
	}
	if gp.m.curg == nil || gp == gp.m.curg {
		goroutineheader(gp)
		traceback(1)
	} else {
		goroutineheader(gp.m.curg)
		// FIXME: Can't do traceback of other g.
	}
	print("\n")
	gp.m.traceback = 0
	unlock(&tracelock)
}

func tracefree(p unsafe.Pointer, size uintptr) {
	lock(&tracelock)
	gp := getg()
	gp.m.traceback = 2
	print("tracefree(", p, ", ", hex(size), ")\n")
	goroutineheader(gp)
	traceback(1)
	print("\n")
	gp.m.traceback = 0
	unlock(&tracelock)
}

func tracegc() {
	lock(&tracelock)
	gp := getg()
	gp.m.traceback = 2
	print("tracegc()\n")
	// running on m->g0 stack; show all non-g0 goroutines
	tracebackothers(gp)
	print("end tracegc\n")
	print("\n")
	gp.m.traceback = 0
	unlock(&tracelock)
}
