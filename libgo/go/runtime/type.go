// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Runtime type representation.

package runtime

import (
	"internal/goarch"
	"runtime/internal/atomic"
	"unsafe"
)

<<<<<<< go/./runtime/type.go
// tflag is documented in reflect/type.go.
//
// tflag values must be kept in sync with copies in:
//	go/types.cc
//	reflect/type.go
//	internal/reflectlite/type.go
type tflag uint8

const (
	tflagRegularMemory tflag = 1 << 3 // equal and hash can treat values of this type as a single region of t.size bytes
)
=======
type nameOff = abi.NameOff
type typeOff = abi.TypeOff
type textOff = abi.TextOff

type _type = abi.Type

// rtype is a wrapper that allows us to define additional methods.
type rtype struct {
	*abi.Type // embedding is okay here (unlike reflect) because none of this is public
}
>>>>>>> /tmp/go121/src/./runtime/type.go

<<<<<<< go/./runtime/type.go
// Needs to be in sync with
// go/types.cc
// ../reflect/type.go:/^type.rtype.
// ../internal/reflectlite/type.go:/^type.rtype.
type _type struct {
	size       uintptr
	ptrdata    uintptr
	hash       uint32
	tflag      tflag
	align      uint8
	fieldAlign uint8
	kind       uint8
	// function for comparing objects of this type
	// (ptr to object A, ptr to object B) -> ==?
	equal func(unsafe.Pointer, unsafe.Pointer) bool
	// gcdata stores the GC type data for the garbage collector.
	// If the KindGCProg bit is set in kind, gcdata is a GC program.
	// Otherwise it is a ptrmask bitmap. See mbitmap.go for details.
	gcdata  *byte
	_string *string
	*uncommontype
	ptrToThis *_type
}

func (t *_type) string() string {
	// For gccgo, try to strip out quoted strings.
	s := *t._string
	q := false
	started := false
	var start int
	var end int
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			q = !q
		} else if !q {
			if !started {
				start = i
				started = true
			}
			end = i
=======
func (t rtype) string() string {
	s := t.nameOff(t.Str).Name()
	if t.TFlag&abi.TFlagExtraStar != 0 {
		return s[1:]
	}
	return s
}

func (t rtype) uncommon() *uncommontype {
	return t.Uncommon()
}

func (t rtype) name() string {
	if t.TFlag&abi.TFlagNamed == 0 {
		return ""
	}
	s := t.string()
	i := len(s) - 1
	sqBrackets := 0
	for i >= 0 && (s[i] != '.' || sqBrackets != 0) {
		switch s[i] {
		case ']':
			sqBrackets++
		case '[':
			sqBrackets--
>>>>>>> /tmp/go121/src/./runtime/type.go
		}
	}
	return s[start : end+1]
}

// pkgpath returns the path of the package where t was defined, if
// available. This is not the same as the reflect package's PkgPath
// method, in that it returns the package path for struct and interface
// types, not just named types.
<<<<<<< go/./runtime/type.go
func (t *_type) pkgpath() string {
	if u := t.uncommontype; u != nil {
		if u.pkgPath == nil {
			return ""
		}
		return *u.pkgPath
=======
func (t rtype) pkgpath() string {
	if u := t.uncommon(); u != nil {
		return t.nameOff(u.PkgPath).Name()
	}
	switch t.Kind_ & kindMask {
	case kindStruct:
		st := (*structtype)(unsafe.Pointer(t.Type))
		return st.PkgPath.Name()
	case kindInterface:
		it := (*interfacetype)(unsafe.Pointer(t.Type))
		return it.PkgPath.Name()
>>>>>>> /tmp/go121/src/./runtime/type.go
	}
	return ""
}

<<<<<<< go/./runtime/type.go
type method struct {
	name    *string
	pkgPath *string
	mtyp    *_type
	typ     *_type
	tfn     unsafe.Pointer
}

type uncommontype struct {
	name    *string
	pkgPath *string
	methods []method
}

type imethod struct {
	name    *string
	pkgPath *string
	typ     *_type
}

type interfacetype struct {
	typ     _type
	methods []imethod
}

type maptype struct {
	typ    _type
	key    *_type
	elem   *_type
	bucket *_type // internal type representing a hash bucket
	// function for hashing keys (ptr to key, seed) -> hash
	hasher     func(unsafe.Pointer, uintptr) uintptr
	keysize    uint8  // size of key slot
	elemsize   uint8  // size of elem slot
	bucketsize uint16 // size of bucket
	flags      uint32
}

// Note: flag values must match those used in the TMAP case
// in ../cmd/compile/internal/reflectdata/reflect.go:writeType.
func (mt *maptype) indirectkey() bool { // store ptr to key instead of key itself
	return mt.flags&1 != 0
}
func (mt *maptype) indirectelem() bool { // store ptr to elem instead of elem itself
	return mt.flags&2 != 0
}
func (mt *maptype) reflexivekey() bool { // true if k==k for all keys
	return mt.flags&4 != 0
}
func (mt *maptype) needkeyupdate() bool { // true if we need to update key on an overwrite
	return mt.flags&8 != 0
}
func (mt *maptype) hashMightPanic() bool { // true if hash function might panic
	return mt.flags&16 != 0
}

type arraytype struct {
	typ   _type
	elem  *_type
	slice *_type
	len   uintptr
}
=======
// reflectOffs holds type offsets defined at run time by the reflect package.
//
// When a type is defined at run time, its *rtype data lives on the heap.
// There are a wide range of possible addresses the heap may use, that
// may not be representable as a 32-bit offset. Moreover the GC may
// one day start moving heap memory, in which case there is no stable
// offset that can be defined.
//
// To provide stable offsets, we add pin *rtype objects in a global map
// and treat the offset as an identifier. We use negative offsets that
// do not overlap with any compile-time module offsets.
//
// Entries are created by reflect.addReflectOff.
var reflectOffs struct {
	lock mutex
	next int32
	m    map[int32]unsafe.Pointer
	minv map[unsafe.Pointer]int32
}

func reflectOffsLock() {
	lock(&reflectOffs.lock)
	if raceenabled {
		raceacquire(unsafe.Pointer(&reflectOffs.lock))
	}
}

func reflectOffsUnlock() {
	if raceenabled {
		racerelease(unsafe.Pointer(&reflectOffs.lock))
	}
	unlock(&reflectOffs.lock)
}

func resolveNameOff(ptrInModule unsafe.Pointer, off nameOff) name {
	if off == 0 {
		return name{}
	}
	base := uintptr(ptrInModule)
	for md := &firstmoduledata; md != nil; md = md.next {
		if base >= md.types && base < md.etypes {
			res := md.types + uintptr(off)
			if res > md.etypes {
				println("runtime: nameOff", hex(off), "out of range", hex(md.types), "-", hex(md.etypes))
				throw("runtime: name offset out of range")
			}
			return name{Bytes: (*byte)(unsafe.Pointer(res))}
		}
	}

	// No module found. see if it is a run time name.
	reflectOffsLock()
	res, found := reflectOffs.m[int32(off)]
	reflectOffsUnlock()
	if !found {
		println("runtime: nameOff", hex(off), "base", hex(base), "not in ranges:")
		for next := &firstmoduledata; next != nil; next = next.next {
			println("\ttypes", hex(next.types), "etypes", hex(next.etypes))
		}
		throw("runtime: name offset base pointer out of range")
	}
	return name{Bytes: (*byte)(res)}
}

func (t rtype) nameOff(off nameOff) name {
	return resolveNameOff(unsafe.Pointer(t.Type), off)
}

func resolveTypeOff(ptrInModule unsafe.Pointer, off typeOff) *_type {
	if off == 0 || off == -1 {
		// -1 is the sentinel value for unreachable code.
		// See cmd/link/internal/ld/data.go:relocsym.
		return nil
	}
	base := uintptr(ptrInModule)
	var md *moduledata
	for next := &firstmoduledata; next != nil; next = next.next {
		if base >= next.types && base < next.etypes {
			md = next
			break
		}
	}
	if md == nil {
		reflectOffsLock()
		res := reflectOffs.m[int32(off)]
		reflectOffsUnlock()
		if res == nil {
			println("runtime: typeOff", hex(off), "base", hex(base), "not in ranges:")
			for next := &firstmoduledata; next != nil; next = next.next {
				println("\ttypes", hex(next.types), "etypes", hex(next.etypes))
			}
			throw("runtime: type offset base pointer out of range")
		}
		return (*_type)(res)
	}
	if t := md.typemap[off]; t != nil {
		return t
	}
	res := md.types + uintptr(off)
	if res > md.etypes {
		println("runtime: typeOff", hex(off), "out of range", hex(md.types), "-", hex(md.etypes))
		throw("runtime: type offset out of range")
	}
	return (*_type)(unsafe.Pointer(res))
}

func (t rtype) typeOff(off typeOff) *_type {
	return resolveTypeOff(unsafe.Pointer(t.Type), off)
}

func (t rtype) textOff(off textOff) unsafe.Pointer {
	if off == -1 {
		// -1 is the sentinel value for unreachable code.
		// See cmd/link/internal/ld/data.go:relocsym.
		return unsafe.Pointer(abi.FuncPCABIInternal(unreachableMethod))
	}
	base := uintptr(unsafe.Pointer(t.Type))
	var md *moduledata
	for next := &firstmoduledata; next != nil; next = next.next {
		if base >= next.types && base < next.etypes {
			md = next
			break
		}
	}
	if md == nil {
		reflectOffsLock()
		res := reflectOffs.m[int32(off)]
		reflectOffsUnlock()
		if res == nil {
			println("runtime: textOff", hex(off), "base", hex(base), "not in ranges:")
			for next := &firstmoduledata; next != nil; next = next.next {
				println("\ttypes", hex(next.types), "etypes", hex(next.etypes))
			}
			throw("runtime: text offset base pointer out of range")
		}
		return res
	}
	res := md.textAddr(uint32(off))
	return unsafe.Pointer(res)
}

type uncommontype = abi.UncommonType
>>>>>>> /tmp/go121/src/./runtime/type.go

type interfacetype = abi.InterfaceType

type maptype = abi.MapType

<<<<<<< go/./runtime/type.go
type functype struct {
	typ       _type
	dotdotdot bool
	in        []*_type
	out       []*_type
}
=======
type arraytype = abi.ArrayType
>>>>>>> /tmp/go121/src/./runtime/type.go

type chantype = abi.ChanType

<<<<<<< go/./runtime/type.go
type structfield struct {
	name       *string // nil for embedded fields
	pkgPath    *string // nil for exported Names; otherwise import path
	typ        *_type  // type of field
	tag        *string // nil if no tag
	offsetAnon uintptr // byte offset of field<<1 | isAnonymous
}
=======
type slicetype = abi.SliceType
>>>>>>> /tmp/go121/src/./runtime/type.go

<<<<<<< go/./runtime/type.go
func (f *structfield) offset() uintptr {
	return f.offsetAnon >> 1
}
=======
type functype = abi.FuncType
>>>>>>> /tmp/go121/src/./runtime/type.go

<<<<<<< go/./runtime/type.go
func (f *structfield) anon() bool {
	return f.offsetAnon&1 != 0
}
=======
type ptrtype = abi.PtrType
>>>>>>> /tmp/go121/src/./runtime/type.go

<<<<<<< go/./runtime/type.go
type structtype struct {
	typ    _type
	fields []structfield
}

// typeDescriptorList holds a list of type descriptors generated
// by the compiler. This is used for the compiler to register
// type descriptors to the runtime.
// The layout is known to the compiler.
//go:notinheap
type typeDescriptorList struct {
	count int
	types [1]uintptr // variable length
}

// typelist holds all type descriptors generated by the comiler.
// This is for the reflect package to deduplicate type descriptors
// when it creates a type that is also a compiler-generated type.
var typelist struct {
	initialized uint32
	lists       []*typeDescriptorList // one element per package
	types       map[string]uintptr    // map from a type's string to *_type, lazily populated
	// TODO: use a sorted array instead?
}
var typelistLock mutex

// The compiler generates a call of this function in the main
// package's init function, to register compiler-generated
// type descriptors.
// p points to a list of *typeDescriptorList, n is the length
// of the list.
//go:linkname registerTypeDescriptors
func registerTypeDescriptors(n int, p unsafe.Pointer) {
	*(*slice)(unsafe.Pointer(&typelist.lists)) = slice{p, n, n}
}

// The reflect package uses this function to look up a compiler-
// generated type descriptor.
//go:linkname reflect_lookupType reflect.lookupType
func reflect_lookupType(s string) *_type {
	// Lazy initialization. We don't need to do this if we never create
	// types through reflection.
	if atomic.Load(&typelist.initialized) == 0 {
		lock(&typelistLock)
		if atomic.Load(&typelist.initialized) == 0 {
			n := 0
			for _, list := range typelist.lists {
				n += list.count
			}
			typelist.types = make(map[string]uintptr, n)
			for _, list := range typelist.lists {
				for i := 0; i < list.count; i++ {
					typ := *(**_type)(add(unsafe.Pointer(&list.types), uintptr(i)*goarch.PtrSize))
					typelist.types[typ.string()] = uintptr(unsafe.Pointer(typ))
=======
type name = abi.Name

type structtype = abi.StructType

func pkgPath(n name) string {
	if n.Bytes == nil || *n.Data(0)&(1<<2) == 0 {
		return ""
	}
	i, l := n.ReadVarint(1)
	off := 1 + i + l
	if *n.Data(0)&(1<<1) != 0 {
		i2, l2 := n.ReadVarint(off)
		off += i2 + l2
	}
	var nameOff nameOff
	copy((*[4]byte)(unsafe.Pointer(&nameOff))[:], (*[4]byte)(unsafe.Pointer(n.Data(off)))[:])
	pkgPathName := resolveNameOff(unsafe.Pointer(n.Bytes), nameOff)
	return pkgPathName.Name()
}

// typelinksinit scans the types from extra modules and builds the
// moduledata typemap used to de-duplicate type pointers.
func typelinksinit() {
	if firstmoduledata.next == nil {
		return
	}
	typehash := make(map[uint32][]*_type, len(firstmoduledata.typelinks))

	modules := activeModules()
	prev := modules[0]
	for _, md := range modules[1:] {
		// Collect types from the previous module into typehash.
	collect:
		for _, tl := range prev.typelinks {
			var t *_type
			if prev.typemap == nil {
				t = (*_type)(unsafe.Pointer(prev.types + uintptr(tl)))
			} else {
				t = prev.typemap[typeOff(tl)]
			}
			// Add to typehash if not seen before.
			tlist := typehash[t.Hash]
			for _, tcur := range tlist {
				if tcur == t {
					continue collect
				}
			}
			typehash[t.Hash] = append(tlist, t)
		}

		if md.typemap == nil {
			// If any of this module's typelinks match a type from a
			// prior module, prefer that prior type by adding the offset
			// to this module's typemap.
			tm := make(map[typeOff]*_type, len(md.typelinks))
			pinnedTypemaps = append(pinnedTypemaps, tm)
			md.typemap = tm
			for _, tl := range md.typelinks {
				t := (*_type)(unsafe.Pointer(md.types + uintptr(tl)))
				for _, candidate := range typehash[t.Hash] {
					seen := map[_typePair]struct{}{}
					if typesEqual(t, candidate, seen) {
						t = candidate
						break
					}
>>>>>>> /tmp/go121/src/./runtime/type.go
				}
			}
			atomic.Store(&typelist.initialized, 1)
		}
		unlock(&typelistLock)
	}

<<<<<<< go/./runtime/type.go
	return (*_type)(unsafe.Pointer(typelist.types[s]))
=======
func toRType(t *abi.Type) rtype {
	return rtype{t}
}

// typesEqual reports whether two types are equal.
//
// Everywhere in the runtime and reflect packages, it is assumed that
// there is exactly one *_type per Go type, so that pointer equality
// can be used to test if types are equal. There is one place that
// breaks this assumption: buildmode=shared. In this case a type can
// appear as two different pieces of memory. This is hidden from the
// runtime and reflect package by the per-module typemap built in
// typelinksinit. It uses typesEqual to map types from later modules
// back into earlier ones.
//
// Only typelinksinit needs this function.
func typesEqual(t, v *_type, seen map[_typePair]struct{}) bool {
	tp := _typePair{t, v}
	if _, ok := seen[tp]; ok {
		return true
	}

	// mark these types as seen, and thus equivalent which prevents an infinite loop if
	// the two types are identical, but recursively defined and loaded from
	// different modules
	seen[tp] = struct{}{}

	if t == v {
		return true
	}
	kind := t.Kind_ & kindMask
	if kind != v.Kind_&kindMask {
		return false
	}
	rt, rv := toRType(t), toRType(v)
	if rt.string() != rv.string() {
		return false
	}
	ut := t.Uncommon()
	uv := v.Uncommon()
	if ut != nil || uv != nil {
		if ut == nil || uv == nil {
			return false
		}
		pkgpatht := rt.nameOff(ut.PkgPath).Name()
		pkgpathv := rv.nameOff(uv.PkgPath).Name()
		if pkgpatht != pkgpathv {
			return false
		}
	}
	if kindBool <= kind && kind <= kindComplex128 {
		return true
	}
	switch kind {
	case kindString, kindUnsafePointer:
		return true
	case kindArray:
		at := (*arraytype)(unsafe.Pointer(t))
		av := (*arraytype)(unsafe.Pointer(v))
		return typesEqual(at.Elem, av.Elem, seen) && at.Len == av.Len
	case kindChan:
		ct := (*chantype)(unsafe.Pointer(t))
		cv := (*chantype)(unsafe.Pointer(v))
		return ct.Dir == cv.Dir && typesEqual(ct.Elem, cv.Elem, seen)
	case kindFunc:
		ft := (*functype)(unsafe.Pointer(t))
		fv := (*functype)(unsafe.Pointer(v))
		if ft.OutCount != fv.OutCount || ft.InCount != fv.InCount {
			return false
		}
		tin, vin := ft.InSlice(), fv.InSlice()
		for i := 0; i < len(tin); i++ {
			if !typesEqual(tin[i], vin[i], seen) {
				return false
			}
		}
		tout, vout := ft.OutSlice(), fv.OutSlice()
		for i := 0; i < len(tout); i++ {
			if !typesEqual(tout[i], vout[i], seen) {
				return false
			}
		}
		return true
	case kindInterface:
		it := (*interfacetype)(unsafe.Pointer(t))
		iv := (*interfacetype)(unsafe.Pointer(v))
		if it.PkgPath.Name() != iv.PkgPath.Name() {
			return false
		}
		if len(it.Methods) != len(iv.Methods) {
			return false
		}
		for i := range it.Methods {
			tm := &it.Methods[i]
			vm := &iv.Methods[i]
			// Note the mhdr array can be relocated from
			// another module. See #17724.
			tname := resolveNameOff(unsafe.Pointer(tm), tm.Name)
			vname := resolveNameOff(unsafe.Pointer(vm), vm.Name)
			if tname.Name() != vname.Name() {
				return false
			}
			if pkgPath(tname) != pkgPath(vname) {
				return false
			}
			tityp := resolveTypeOff(unsafe.Pointer(tm), tm.Typ)
			vityp := resolveTypeOff(unsafe.Pointer(vm), vm.Typ)
			if !typesEqual(tityp, vityp, seen) {
				return false
			}
		}
		return true
	case kindMap:
		mt := (*maptype)(unsafe.Pointer(t))
		mv := (*maptype)(unsafe.Pointer(v))
		return typesEqual(mt.Key, mv.Key, seen) && typesEqual(mt.Elem, mv.Elem, seen)
	case kindPtr:
		pt := (*ptrtype)(unsafe.Pointer(t))
		pv := (*ptrtype)(unsafe.Pointer(v))
		return typesEqual(pt.Elem, pv.Elem, seen)
	case kindSlice:
		st := (*slicetype)(unsafe.Pointer(t))
		sv := (*slicetype)(unsafe.Pointer(v))
		return typesEqual(st.Elem, sv.Elem, seen)
	case kindStruct:
		st := (*structtype)(unsafe.Pointer(t))
		sv := (*structtype)(unsafe.Pointer(v))
		if len(st.Fields) != len(sv.Fields) {
			return false
		}
		if st.PkgPath.Name() != sv.PkgPath.Name() {
			return false
		}
		for i := range st.Fields {
			tf := &st.Fields[i]
			vf := &sv.Fields[i]
			if tf.Name.Name() != vf.Name.Name() {
				return false
			}
			if !typesEqual(tf.Typ, vf.Typ, seen) {
				return false
			}
			if tf.Name.Tag() != vf.Name.Tag() {
				return false
			}
			if tf.Offset != vf.Offset {
				return false
			}
			if tf.Name.IsEmbedded() != vf.Name.IsEmbedded() {
				return false
			}
		}
		return true
	default:
		println("runtime: impossible type kind", kind)
		throw("runtime: impossible type kind")
		return false
	}
>>>>>>> /tmp/go121/src/./runtime/type.go
}
