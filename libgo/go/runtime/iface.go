// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/goarch"
	"runtime/internal/atomic"
	"unsafe"
)

// For gccgo, use go:linkname to export compiler-called functions.
//
//go:linkname requireitab
//go:linkname assertitab
//go:linkname panicdottype
//go:linkname ifaceE2E2
//go:linkname ifaceI2E2
//go:linkname ifaceE2I2
//go:linkname ifaceI2I2
//go:linkname ifaceE2T2P
//go:linkname ifaceI2T2P
//go:linkname ifaceE2T2
//go:linkname ifaceI2T2
//go:linkname ifaceT2Ip
// Temporary for C code to call:
//go:linkname getitab

// The gccgo itab structure is different than the gc one.
//
// Both gccgo and gc represent empty interfaces the same way:
// a two field struct, where the first field points to a type descriptor
// (a *_type) and the second field is the data pointer.
//
// Non-empty interfaces are also two-field structs, and the second
// field is the data pointer. However, for gccgo, the first field, the
// itab field, is different. The itab field points to the interface
// method table, which is the implemention of a specific interface
// type for a specific dynamic non-interface type.  An interface
// method table is a list of pointer values. The first pointer is the
// type descriptor (a *_type) for the dynamic type. The subsequent
// pointers are pointers to function code, which implement the methods
// required by the interface. The pointers are sorted by name.
//
// The method pointers in the itab are C function pointers, not Go
// function pointers; they may be called directly, and they have no
// closures. The receiver is always passed as a pointer, and it is
// always the same pointer stored in the interface value. A value
// method starts by copying the receiver value out of the pointer into
// a local variable.
//
// A method call on an interface value is by definition calling a
// method at a known index m in the list of methods. Given a non-empty
// interface value i, the call i.m(args) looks like
//     i.itab[m+1](i.iface, args)

// Both an empty interface and a non-empty interface have a data
// pointer field. The meaning of this field is determined by the
// kindDirectIface bit in the `kind` field of the type descriptor of
// the value stored in the interface. If kindDirectIface is set, then
// the data pointer field in the interface value is exactly the value
// stored in the interface. Otherwise, the data pointer field is a
// pointer to memory that holds the value. It follows from this that
// kindDirectIface can only be set for a type whose representation is
// simply a pointer. In the current gccgo implementation, this is set
// for types that are pointer-shaped, including unsafe.Pointer, channels,
// maps, functions, single-field structs and single-element arrays whose
// single field is simply a pointer-shaped type.

// For a nil interface value both fields in the interface struct are nil.

// itabs are statically allocated or persistently allocated. They are
// never freed. For itabs allocated at run time, they are cached in
// itabTable, so we reuse the same itab for the same (interface, concrete)
// type pair. The gc runtime prepopulates the cache with statically
// allocated itabs. Currently we don't do that as we don't have a way to
// find all the statically allocated itabs.

const itabInitSize = 512

var (
	itabLock      mutex                               // lock for accessing itab table
	itabTable     = &itabTableInit                    // pointer to current table
	itabTableInit = itabTableType{size: itabInitSize} // starter table
)

// Cache entry type of itab table.
// For gccgo, this is not the data type we used in the interface header.
type itab struct {
	inter   *interfacetype
	methods [2]unsafe.Pointer // method table. variable sized. first entry is the type descriptor.
}

func (m *itab) _type() *_type {
	return (*_type)(m.methods[0])
}

// Note: change the formula in the mallocgc call in itabAdd if you change these fields.
type itabTableType struct {
	size    uintptr             // length of entries array. Always a power of 2.
	count   uintptr             // current number of filled entries.
	entries [itabInitSize]*itab // really [size] large
}

func itabHashFunc(inter *interfacetype, typ *_type) uintptr {
	// compiler has provided some good hash codes for us.
	return uintptr(inter.Type.Hash ^ typ.Hash)
}

<<<<<<< go/./runtime/iface.go
=======
func getitab(inter *interfacetype, typ *_type, canfail bool) *itab {
	if len(inter.Methods) == 0 {
		throw("internal error - misuse of itab")
	}

	// easy case
	if typ.TFlag&abi.TFlagUncommon == 0 {
		if canfail {
			return nil
		}
		name := toRType(&inter.Type).nameOff(inter.Methods[0].Name)
		panic(&TypeAssertionError{nil, typ, &inter.Type, name.Name()})
	}

	var m *itab

	// First, look in the existing table to see if we can find the itab we need.
	// This is by far the most common case, so do it without locks.
	// Use atomic to ensure we see any previous writes done by the thread
	// that updates the itabTable field (with atomic.Storep in itabAdd).
	t := (*itabTableType)(atomic.Loadp(unsafe.Pointer(&itabTable)))
	if m = t.find(inter, typ); m != nil {
		goto finish
	}

	// Not found.  Grab the lock and try again.
	lock(&itabLock)
	if m = itabTable.find(inter, typ); m != nil {
		unlock(&itabLock)
		goto finish
	}

	// Entry doesn't exist yet. Make a new entry & add it.
	m = (*itab)(persistentalloc(unsafe.Sizeof(itab{})+uintptr(len(inter.Methods)-1)*goarch.PtrSize, 0, &memstats.other_sys))
	m.inter = inter
	m._type = typ
	// The hash is used in type switches. However, compiler statically generates itab's
	// for all interface/type pairs used in switches (which are added to itabTable
	// in itabsinit). The dynamically-generated itab's never participate in type switches,
	// and thus the hash is irrelevant.
	// Note: m.hash is _not_ the hash used for the runtime itabTable hash table.
	m.hash = 0
	m.init()
	itabAdd(m)
	unlock(&itabLock)
finish:
	if m.fun[0] != 0 {
		return m
	}
	if canfail {
		return nil
	}
	// this can only happen if the conversion
	// was already done once using the , ok form
	// and we have a cached negative result.
	// The cached result doesn't record which
	// interface function was missing, so initialize
	// the itab again to get the missing function name.
	panic(&TypeAssertionError{concrete: typ, asserted: &inter.Type, missingMethod: m.init()})
}

>>>>>>> /tmp/go121/src/./runtime/iface.go
// find finds the given interface/type pair in t.
// Returns nil if the given interface/type pair isn't present.
func (t *itabTableType) find(inter *interfacetype, typ *_type) *itab {
	// Implemented using quadratic probing.
	// Probe sequence is h(i) = h0 + i*(i+1)/2 mod 2^k.
	// We're guaranteed to hit all table entries using this probe sequence.
	mask := t.size - 1
	h := itabHashFunc(inter, typ) & mask
	for i := uintptr(1); ; i++ {
		p := (**itab)(add(unsafe.Pointer(&t.entries), h*goarch.PtrSize))
		// Use atomic read here so if we see m != nil, we also see
		// the initializations of the fields of m.
		// m := *p
		m := (*itab)(atomic.Loadp(unsafe.Pointer(p)))
		if m == nil {
			return nil
		}
		if m.inter == inter && m._type() == typ {
			return m
		}
		h += i
		h &= mask
	}
}

// itabAdd adds the given itab to the itab hash table.
// itabLock must be held.
func itabAdd(m *itab) {
	// Bugs can lead to calling this while mallocing is set,
	// typically because this is called while panicking.
	// Crash reliably, rather than only when we need to grow
	// the hash table.
	if getg().m.mallocing != 0 {
		throw("malloc deadlock")
	}

	t := itabTable
	if t.count >= 3*(t.size/4) { // 75% load factor
		// Grow hash table.
		// t2 = new(itabTableType) + some additional entries
		// We lie and tell malloc we want pointer-free memory because
		// all the pointed-to values are not in the heap.
		t2 := (*itabTableType)(mallocgc((2+2*t.size)*goarch.PtrSize, nil, true))
		t2.size = t.size * 2

		// Copy over entries.
		// Note: while copying, other threads may look for an itab and
		// fail to find it. That's ok, they will then try to get the itab lock
		// and as a consequence wait until this copying is complete.
		iterate_itabs(t2.add)
		if t2.count != t.count {
			throw("mismatched count during itab table copy")
		}
		// Publish new hash table. Use an atomic write: see comment in getitab.
		atomicstorep(unsafe.Pointer(&itabTable), unsafe.Pointer(t2))
		// Adopt the new table as our own.
		t = itabTable
		// Note: the old table can be GC'ed here.
	}
	t.add(m)
}

// add adds the given itab to itab table t.
// itabLock must be held.
func (t *itabTableType) add(m *itab) {
	// See comment in find about the probe sequence.
	// Insert new itab in the first empty spot in the probe sequence.
	mask := t.size - 1
	h := itabHashFunc(m.inter, m._type()) & mask
	for i := uintptr(1); ; i++ {
		p := (**itab)(add(unsafe.Pointer(&t.entries), h*goarch.PtrSize))
		m2 := *p
		if m2 == m {
			// A given itab may be used in more than one module
			// and thanks to the way global symbol resolution works, the
			// pointed-to itab may already have been inserted into the
			// global 'hash'.
			return
		}
		if m2 == nil {
			// Use atomic write here so if a reader sees m, it also
			// sees the correctly initialized fields of m.
			// NoWB is ok because m is not in heap memory.
			// *p = m
			atomic.StorepNoWB(unsafe.Pointer(p), unsafe.Pointer(m))
			t.count++
			return
		}
		h += i
		h &= mask
	}
}

// init fills in the m.methods array with all the code pointers for
// the m.inter/m._type pair. If the type does not implement the interface,
// it sets m.methods[1] to nil and returns the name of an interface function that is missing.
// It is ok to call this multiple times on the same m, even concurrently.
func (m *itab) init() string {
	inter := m.inter
<<<<<<< go/./runtime/iface.go
	typ := m._type()
	ni := len(inter.methods) + 1
	methods := (*[1 << 16]unsafe.Pointer)(unsafe.Pointer(&m.methods[0]))[:ni:ni]
	var m1 unsafe.Pointer

	ri := 0
	for li := range inter.methods {
		lhsMethod := &inter.methods[li]
		var rhsMethod *method

		for {
			if ri >= len(typ.methods) {
				m.methods[1] = nil
				return *lhsMethod.name
			}
=======
	typ := m._type
	x := typ.Uncommon()
>>>>>>> /tmp/go121/src/./runtime/iface.go

<<<<<<< go/./runtime/iface.go
			rhsMethod = &typ.methods[ri]
			if (lhsMethod.name == rhsMethod.name || *lhsMethod.name == *rhsMethod.name) &&
				(lhsMethod.pkgPath == rhsMethod.pkgPath || *lhsMethod.pkgPath == *rhsMethod.pkgPath) {
				break
=======
	// both inter and typ have method sorted by name,
	// and interface names are unique,
	// so can iterate over both in lock step;
	// the loop is O(ni+nt) not O(ni*nt).
	ni := len(inter.Methods)
	nt := int(x.Mcount)
	xmhdr := (*[1 << 16]abi.Method)(add(unsafe.Pointer(x), uintptr(x.Moff)))[:nt:nt]
	j := 0
	methods := (*[1 << 16]unsafe.Pointer)(unsafe.Pointer(&m.fun[0]))[:ni:ni]
	var fun0 unsafe.Pointer
imethods:
	for k := 0; k < ni; k++ {
		i := &inter.Methods[k]
		itype := toRType(&inter.Type).typeOff(i.Typ)
		name := toRType(&inter.Type).nameOff(i.Name)
		iname := name.Name()
		ipkg := pkgPath(name)
		if ipkg == "" {
			ipkg = inter.PkgPath.Name()
		}
		for ; j < nt; j++ {
			t := &xmhdr[j]
			rtyp := toRType(typ)
			tname := rtyp.nameOff(t.Name)
			if rtyp.typeOff(t.Mtyp) == itype && tname.Name() == iname {
				pkgPath := pkgPath(tname)
				if pkgPath == "" {
					pkgPath = rtyp.nameOff(x.PkgPath).Name()
				}
				if tname.IsExported() || pkgPath == ipkg {
					if m != nil {
						ifn := rtyp.textOff(t.Ifn)
						if k == 0 {
							fun0 = ifn // we'll set m.fun[0] at the end
						} else {
							methods[k] = ifn
						}
					}
					continue imethods
				}
>>>>>>> /tmp/go121/src/./runtime/iface.go
			}

			ri++
		}

		if !eqtype(lhsMethod.typ, rhsMethod.mtyp) {
			m.methods[1] = nil
			return *lhsMethod.name
		}

		if li == 0 {
			m1 = rhsMethod.tfn // we'll set m.methods[1] at the end
		} else {
			methods[li+1] = rhsMethod.tfn
		}
		ri++
	}
	m.methods[1] = m1
	return ""
}

func iterate_itabs(fn func(*itab)) {
	// Note: only runs during stop the world or with itabLock held,
	// so no other locks/atomics needed.
	t := itabTable
	for i := uintptr(0); i < t.size; i++ {
		m := *(**itab)(add(unsafe.Pointer(&t.entries), i*goarch.PtrSize))
		if m != nil {
			fn(m)
		}
	}
}

// Return the interface method table for a value of type rhs converted
// to an interface of type lhs.
func getitab(lhs, rhs *_type, canfail bool) unsafe.Pointer {
	if rhs == nil {
		return nil
	}

	if lhs.kind&kindMask != kindInterface {
		throw("getitab called for non-interface type")
	}

	lhsi := (*interfacetype)(unsafe.Pointer(lhs))

	if len(lhsi.methods) == 0 {
		throw("getitab called for empty interface type")
	}

	if rhs.uncommontype == nil || len(rhs.methods) == 0 {
		if canfail {
			return nil
		}
		panic(&TypeAssertionError{nil, rhs, lhs, *lhsi.methods[0].name})
	}

	var m *itab

	// First, look in the existing table to see if we can find the itab we need.
	// This is by far the most common case, so do it without locks.
	// Use atomic to ensure we see any previous writes done by the thread
	// that updates the itabTable field (with atomic.Storep in itabAdd).
	t := (*itabTableType)(atomic.Loadp(unsafe.Pointer(&itabTable)))
	if m = t.find(lhsi, rhs); m != nil {
		goto finish
	}

	// Not found.  Grab the lock and try again.
	lockInit(&itabLock, lockRankItab)
	lock(&itabLock)
	if m = itabTable.find(lhsi, rhs); m != nil {
		unlock(&itabLock)
		goto finish
	}

	// Entry doesn't exist yet. Make a new entry & add it.
	m = (*itab)(persistentalloc(unsafe.Sizeof(itab{})+uintptr(len(lhsi.methods)-1)*goarch.PtrSize, 0, &memstats.other_sys))
	m.inter = lhsi
	m.methods[0] = unsafe.Pointer(rhs)
	m.init()
	itabAdd(m)
	unlock(&itabLock)
finish:
	if m.methods[1] != nil {
		return unsafe.Pointer(&m.methods[0])
	}
	if canfail {
		return nil
	}
	// this can only happen if the conversion
	// was already done once using the , ok form
	// and we have a cached negative result.
	// The cached result doesn't record which
	// interface function was missing, so initialize
	// the itab again to get the missing function name.
	panic(&TypeAssertionError{nil, rhs, lhs, m.init()})
}

<<<<<<< go/./runtime/iface.go
// Return the interface method table for a value of type rhs converted
// to an interface of type lhs.  Panics if the conversion is impossible.
func requireitab(lhs, rhs *_type) unsafe.Pointer {
	return getitab(lhs, rhs, false)
}

// Return the interface method table for a value of type rhs converted
// to an interface of type lhs.  Panics if the conversion is
// impossible or if the rhs type is nil.
func assertitab(lhs, rhs *_type) unsafe.Pointer {
	if rhs == nil {
		panic(&TypeAssertionError{nil, nil, lhs, ""})
	}

	if lhs.kind&kindMask != kindInterface {
		throw("assertitab called for non-interface type")
	}

	lhsi := (*interfacetype)(unsafe.Pointer(lhs))

	if len(lhsi.methods) == 0 {
		return unsafe.Pointer(rhs)
	}

	return getitab(lhs, rhs, false)
}

// panicdottype is called when doing an i.(T) conversion and the conversion fails.
func panicdottype(lhs, rhs, inter *_type) {
	panic(&TypeAssertionError{inter, rhs, lhs, ""})
}
=======
// panicdottypeE is called when doing an e.(T) conversion and the conversion fails.
// have = the dynamic type we have.
// want = the static type we're trying to convert to.
// iface = the static type we're converting from.
func panicdottypeE(have, want, iface *_type) {
	panic(&TypeAssertionError{iface, have, want, ""})
}

// panicdottypeI is called when doing an i.(T) conversion and the conversion fails.
// Same args as panicdottypeE, but "have" is the dynamic itab we have.
func panicdottypeI(have *itab, want, iface *_type) {
	var t *_type
	if have != nil {
		t = have._type
	}
	panicdottypeE(t, want, iface)
}

// panicnildottype is called when doing an i.(T) conversion and the interface i is nil.
// want = the static type we're trying to convert to.
func panicnildottype(want *_type) {
	panic(&TypeAssertionError{nil, nil, want, ""})
	// TODO: Add the static type we're converting from as well.
	// It might generate a better error message.
	// Just to match other nil conversion errors, we don't for now.
}

// The specialized convTx routines need a type descriptor to use when calling mallocgc.
// We don't need the type to be exact, just to have the correct size, alignment, and pointer-ness.
// However, when debugging, it'd be nice to have some indication in mallocgc where the types came from,
// so we use named types here.
// We then construct interface values of these types,
// and then extract the type word to use as needed.
type (
	uint16InterfacePtr uint16
	uint32InterfacePtr uint32
	uint64InterfacePtr uint64
	stringInterfacePtr string
	sliceInterfacePtr  []byte
)
>>>>>>> /tmp/go121/src/./runtime/iface.go

// Convert an empty interface to an empty interface, for a comma-ok
// type assertion.
func ifaceE2E2(e eface) (eface, bool) {
	return e, e._type != nil
}

<<<<<<< go/./runtime/iface.go
// Convert a non-empty interface to an empty interface, for a comma-ok
// type assertion.
func ifaceI2E2(i iface) (eface, bool) {
	if i.tab == nil {
		return eface{nil, nil}, false
=======
// The conv and assert functions below do very similar things.
// The convXXX functions are guaranteed by the compiler to succeed.
// The assertXXX functions may fail (either panicking or returning false,
// depending on whether they are 1-result or 2-result).
// The convXXX functions succeed on a nil input, whereas the assertXXX
// functions fail on a nil input.

// convT converts a value of type t, which is pointed to by v, to a pointer that can
// be used as the second word of an interface value.
func convT(t *_type, v unsafe.Pointer) unsafe.Pointer {
	if raceenabled {
		raceReadObjectPC(t, v, getcallerpc(), abi.FuncPCABIInternal(convT))
	}
	if msanenabled {
		msanread(v, t.Size_)
	}
	if asanenabled {
		asanread(v, t.Size_)
	}
	x := mallocgc(t.Size_, t, true)
	typedmemmove(t, x, v)
	return x
}
func convTnoptr(t *_type, v unsafe.Pointer) unsafe.Pointer {
	// TODO: maybe take size instead of type?
	if raceenabled {
		raceReadObjectPC(t, v, getcallerpc(), abi.FuncPCABIInternal(convTnoptr))
	}
	if msanenabled {
		msanread(v, t.Size_)
	}
	if asanenabled {
		asanread(v, t.Size_)
	}

	x := mallocgc(t.Size_, t, false)
	memmove(x, v, t.Size_)
	return x
}

func convT16(val uint16) (x unsafe.Pointer) {
	if val < uint16(len(staticuint64s)) {
		x = unsafe.Pointer(&staticuint64s[val])
		if goarch.BigEndian {
			x = add(x, 6)
		}
>>>>>>> /tmp/go121/src/./runtime/iface.go
	} else {
		return eface{*(**_type)(i.tab), i.data}, true
	}
}

// Convert an empty interface to a non-empty interface, for a comma-ok
// type assertion.
func ifaceE2I2(inter *_type, e eface) (iface, bool) {
	if e._type == nil {
		return iface{nil, nil}, false
	} else {
		itab := getitab(inter, e._type, true)
		if itab == nil {
			return iface{nil, nil}, false
		} else {
			return iface{itab, e.data}, true
		}
	}
}

// Convert a non-empty interface to a non-empty interface, for a
// comma-ok type assertion.
func ifaceI2I2(inter *_type, i iface) (iface, bool) {
	if i.tab == nil {
		return iface{nil, nil}, false
	} else {
		itab := getitab(inter, *(**_type)(i.tab), true)
		if itab == nil {
			return iface{nil, nil}, false
		} else {
			return iface{itab, i.data}, true
		}
	}
}

// Convert an empty interface to a pointer non-interface type.
func ifaceE2T2P(t *_type, e eface) (unsafe.Pointer, bool) {
	if !eqtype(t, e._type) {
		return nil, false
	} else {
		return e.data, true
	}
}

// Convert a non-empty interface to a pointer non-interface type.
func ifaceI2T2P(t *_type, i iface) (unsafe.Pointer, bool) {
	if i.tab == nil || !eqtype(t, *(**_type)(i.tab)) {
		return nil, false
	} else {
		return i.data, true
	}
}

// Convert an empty interface to a non-pointer non-interface type.
func ifaceE2T2(t *_type, e eface, ret unsafe.Pointer) bool {
	if !eqtype(t, e._type) {
		typedmemclr(t, ret)
		return false
	} else {
		if isDirectIface(t) {
			*(*unsafe.Pointer)(ret) = e.data
		} else {
			typedmemmove(t, ret, e.data)
		}
		return true
	}
}

// Convert a non-empty interface to a non-pointer non-interface type.
func ifaceI2T2(t *_type, i iface, ret unsafe.Pointer) bool {
	if i.tab == nil || !eqtype(t, *(**_type)(i.tab)) {
		typedmemclr(t, ret)
		return false
	} else {
		if isDirectIface(t) {
			*(*unsafe.Pointer)(ret) = i.data
		} else {
			typedmemmove(t, ret, i.data)
		}
		return true
	}
}

<<<<<<< go/./runtime/iface.go
// Return whether we can convert a type to an interface type.
func ifaceT2Ip(to, from *_type) bool {
	if from == nil {
		return false
=======
func assertI2I(inter *interfacetype, tab *itab) *itab {
	if tab == nil {
		// explicit conversions require non-nil interface value.
		panic(&TypeAssertionError{nil, nil, &inter.Type, ""})
>>>>>>> /tmp/go121/src/./runtime/iface.go
	}

	if to.kind&kindMask != kindInterface {
		throw("ifaceT2Ip called with non-interface type")
	}
	toi := (*interfacetype)(unsafe.Pointer(to))

	if from.uncommontype == nil || len(from.methods) == 0 {
		return len(toi.methods) == 0
	}

	ri := 0
	for li := range toi.methods {
		toMethod := &toi.methods[li]
		var fromMethod *method
		for {
			if ri >= len(from.methods) {
				return false
			}

			fromMethod = &from.methods[ri]
			if (toMethod.name == fromMethod.name || *toMethod.name == *fromMethod.name) &&
				(toMethod.pkgPath == fromMethod.pkgPath || *toMethod.pkgPath == *fromMethod.pkgPath) {
				break
			}

			ri++
		}

		if !eqtype(fromMethod.mtyp, toMethod.typ) {
			return false
		}

		ri++
	}

<<<<<<< go/./runtime/iface.go
	return true
=======
func assertE2I(inter *interfacetype, t *_type) *itab {
	if t == nil {
		// explicit conversions require non-nil interface value.
		panic(&TypeAssertionError{nil, nil, &inter.Type, ""})
	}
	return getitab(inter, t, false)
>>>>>>> /tmp/go121/src/./runtime/iface.go
}

//go:linkname reflect_ifaceE2I reflect.ifaceE2I
func reflect_ifaceE2I(inter *interfacetype, e eface, dst *iface) {
	t := e._type
	if t == nil {
		panic(TypeAssertionError{nil, nil, &inter.typ, ""})
	}
	dst.tab = requireitab((*_type)(unsafe.Pointer(inter)), t)
	dst.data = e.data
}

//go:linkname reflectlite_ifaceE2I internal_1reflectlite.ifaceE2I
func reflectlite_ifaceE2I(inter *interfacetype, e eface, dst *iface) {
	t := e._type
	if t == nil {
		panic(TypeAssertionError{nil, nil, &inter.typ, ""})
	}
	dst.tab = requireitab((*_type)(unsafe.Pointer(inter)), t)
	dst.data = e.data
}

// staticuint64s is used to avoid allocating in convTx for small integer values.
var staticuint64s = [...]uint64{
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
	0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
	0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
	0x28, 0x29, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f,
	0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
	0x38, 0x39, 0x3a, 0x3b, 0x3c, 0x3d, 0x3e, 0x3f,
	0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47,
	0x48, 0x49, 0x4a, 0x4b, 0x4c, 0x4d, 0x4e, 0x4f,
	0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57,
	0x58, 0x59, 0x5a, 0x5b, 0x5c, 0x5d, 0x5e, 0x5f,
	0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67,
	0x68, 0x69, 0x6a, 0x6b, 0x6c, 0x6d, 0x6e, 0x6f,
	0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77,
	0x78, 0x79, 0x7a, 0x7b, 0x7c, 0x7d, 0x7e, 0x7f,
	0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
	0x88, 0x89, 0x8a, 0x8b, 0x8c, 0x8d, 0x8e, 0x8f,
	0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97,
	0x98, 0x99, 0x9a, 0x9b, 0x9c, 0x9d, 0x9e, 0x9f,
	0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
	0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf,
	0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7,
	0xb8, 0xb9, 0xba, 0xbb, 0xbc, 0xbd, 0xbe, 0xbf,
	0xc0, 0xc1, 0xc2, 0xc3, 0xc4, 0xc5, 0xc6, 0xc7,
	0xc8, 0xc9, 0xca, 0xcb, 0xcc, 0xcd, 0xce, 0xcf,
	0xd0, 0xd1, 0xd2, 0xd3, 0xd4, 0xd5, 0xd6, 0xd7,
	0xd8, 0xd9, 0xda, 0xdb, 0xdc, 0xdd, 0xde, 0xdf,
	0xe0, 0xe1, 0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7,
	0xe8, 0xe9, 0xea, 0xeb, 0xec, 0xed, 0xee, 0xef,
	0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7,
	0xf8, 0xf9, 0xfa, 0xfb, 0xfc, 0xfd, 0xfe, 0xff,
}
