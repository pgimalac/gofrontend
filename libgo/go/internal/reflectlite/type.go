// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package reflectlite implements lightweight version of reflect, not using
// any package except for "runtime", "unsafe", and "internal/abi"
package reflectlite

<<<<<<< go/./internal/reflectlite/type.go
import (
	"unsafe"
)
=======
import (
	"internal/abi"
	"unsafe"
)
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go

// Type is the representation of a Go type.
//
// Not all methods apply to all kinds of types. Restrictions,
// if any, are noted in the documentation for each method.
// Use the Kind method to find out the kind of type before
// calling kind-specific methods. Calling a method
// inappropriate to the kind of type causes a run-time panic.
//
// Type values are comparable, such as with the == operator,
// so they can be used as map keys.
// Two Type values are equal if they represent identical types.
type Type interface {
	// Methods applicable to all types.

	// Name returns the type's name within its package for a defined type.
	// For other (non-defined) types it returns the empty string.
	Name() string

	// PkgPath returns a defined type's package path, that is, the import path
	// that uniquely identifies the package, such as "encoding/base64".
	// If the type was predeclared (string, error) or not defined (*T, struct{},
	// []int, or A where A is an alias for a non-defined type), the package path
	// will be the empty string.
	PkgPath() string

	// Size returns the number of bytes needed to store
	// a value of the given type; it is analogous to unsafe.Sizeof.
	Size() uintptr

	// Kind returns the specific kind of this type.
	Kind() Kind

	// Implements reports whether the type implements the interface type u.
	Implements(u Type) bool

	// AssignableTo reports whether a value of the type is assignable to type u.
	AssignableTo(u Type) bool

	// Comparable reports whether values of this type are comparable.
	Comparable() bool

	// String returns a string representation of the type.
	// The string representation may use shortened package names
	// (e.g., base64 instead of "encoding/base64") and is not
	// guaranteed to be unique among types. To test for type identity,
	// compare the Types directly.
	String() string

	// Elem returns a type's element type.
	// It panics if the type's Kind is not Ptr.
	Elem() Type

	common() *abi.Type
	uncommon() *uncommonType
}

/*
 * These data structures are known to the compiler (../../cmd/internal/reflectdata/reflect.go).
 * A few are known to ../runtime/type.go to convey to debuggers.
 * They are also known to ../runtime/type.go.
 */

// A Kind represents the specific kind of type that a Type represents.
// The zero Kind is not a valid kind.
type Kind = abi.Kind

const Ptr = abi.Pointer

const (
	// Import-and-export these constants as necessary
	Interface = abi.Interface
	Slice     = abi.Slice
	String    = abi.String
	Struct    = abi.Struct
)

<<<<<<< go/./internal/reflectlite/type.go
const Ptr = Pointer

// tflag is used by an rtype to signal what extra type information is
// available in the memory directly following the rtype value.
//
// tflag values must be kept in sync with copies in:
//	go/types.cc
//	runtime/type.go
type tflag uint8

const (
	// tflagRegularMemory means that equal and hash functions can treat
	// this type as a single region of t.size bytes.
	tflagRegularMemory tflag = 1 << 3
)
=======
type nameOff = abi.NameOff
type typeOff = abi.TypeOff
type textOff = abi.TextOff
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go

type rtype struct {
<<<<<<< go/./internal/reflectlite/type.go
	size       uintptr
	ptrdata    uintptr // number of bytes in the type that can contain pointers
	hash       uint32  // hash of type; avoids computation in hash tables
	tflag      tflag   // extra type information flags
	align      uint8   // alignment of variable with this type
	fieldAlign uint8   // alignment of struct field with this type
	kind       uint8   // enumeration for C
	// function for comparing objects of this type
	// (ptr to object A, ptr to object B) -> ==?
	equal         func(unsafe.Pointer, unsafe.Pointer) bool
	gcdata        *byte   // garbage collection data
	string        *string // string form; unnecessary but undeniably useful
	*uncommonType         // (relatively) uncommon fields
	ptrToThis     *rtype  // type for pointer to this type, may be zero
}

// Method on non-interface type
type method struct {
	name    *string        // name of method
	pkgPath *string        // nil for exported Names; otherwise import path
	mtyp    *rtype         // method type (without receiver)
	typ     *rtype         // .(*FuncType) underneath (with receiver)
	tfn     unsafe.Pointer // fn used for normal method call
=======
	*abi.Type
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

// uncommonType is present only for defined types or types with methods
// (if T is a defined type, the uncommonTypes for T and *T have methods).
// Using a pointer to this struct reduces the overall size required
// to describe a non-defined type with no methods.
<<<<<<< go/./internal/reflectlite/type.go
type uncommonType struct {
	name    *string  // name of type
	pkgPath *string  // import path; nil for built-in types like int, string
	methods []method // methods associated with type
}

// chanDir represents a channel type's direction.
type chanDir int

const (
	recvDir chanDir             = 1 << iota // <-chan
	sendDir                                 // chan<-
	bothDir = recvDir | sendDir             // chan
)
=======
type uncommonType = abi.UncommonType
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go

// arrayType represents a fixed array type.
type arrayType = abi.ArrayType

// chanType represents a channel type.
<<<<<<< go/./internal/reflectlite/type.go
type chanType struct {
	rtype
	elem *rtype  // channel element type
	dir  uintptr // channel direction (chanDir)
}

// funcType represents a function type.
type funcType struct {
	rtype
	dotdotdot bool     // last input parameter is ...
	in        []*rtype // input parameter types
	out       []*rtype // output parameter types
}
=======
type chanType = abi.ChanType
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go

<<<<<<< go/./internal/reflectlite/type.go
// imethod represents a method on an interface type
type imethod struct {
	name    *string // name of method
	pkgPath *string // nil for exported Names; otherwise import path
	typ     *rtype  // .(*FuncType) underneath
}
=======
type funcType = abi.FuncType
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go

<<<<<<< go/./internal/reflectlite/type.go
// interfaceType represents an interface type.
type interfaceType struct {
	rtype
	methods []imethod // sorted by hash
}
=======
type interfaceType = abi.InterfaceType
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go

// mapType represents a map type.
type mapType struct {
	rtype
<<<<<<< go/./internal/reflectlite/type.go
	key        *rtype // map key type
	elem       *rtype // map element (value) type
	bucket     *rtype // internal bucket structure
	keysize    uint8  // size of key slot
	valuesize  uint8  // size of value slot
	bucketsize uint16 // size of bucket
	flags      uint32
=======
	Key    *abi.Type // map key type
	Elem   *abi.Type // map element (value) type
	Bucket *abi.Type // internal bucket structure
	// function for hashing keys (ptr to key, seed) -> hash
	Hasher     func(unsafe.Pointer, uintptr) uintptr
	KeySize    uint8  // size of key slot
	ValueSize  uint8  // size of value slot
	BucketSize uint16 // size of bucket
	Flags      uint32
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

// ptrType represents a pointer type.
type ptrType = abi.PtrType

// sliceType represents a slice type.
<<<<<<< go/./internal/reflectlite/type.go
type sliceType struct {
	rtype
	elem *rtype // slice element type
}

// Struct field
type structField struct {
	name        *string // name is always non-empty
	pkgPath     *string // nil for exported Names; otherwise import path
	typ         *rtype  // type of field
	tag         *string // nil if no tag
	offsetEmbed uintptr // byte offset of field<<1 | isAnonymous
}

func (f *structField) offset() uintptr {
	return f.offsetEmbed >> 1
}

func (f *structField) embedded() bool {
	return f.offsetEmbed&1 != 0
}
=======
type sliceType = abi.SliceType
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go

// structType represents a struct type.
<<<<<<< go/./internal/reflectlite/type.go
type structType struct {
	rtype
	fields []structField // sorted by offset
=======
type structType = abi.StructType

// name is an encoded type name with optional extra data.
//
// The first byte is a bit field containing:
//
//	1<<0 the name is exported
//	1<<1 tag data follows the name
//	1<<2 pkgPath nameOff follows the name and tag
//
// The next two bytes are the data length:
//
//	l := uint16(data[1])<<8 | uint16(data[2])
//
// Bytes [3:3+l] are the string data.
//
// If tag data follows then bytes 3+l and 3+l+1 are the tag length,
// with the data following.
//
// If the import path follows, then 4 bytes at the end of
// the data form a nameOff. The import path is only set for concrete
// methods that are defined in a different package than their type.
//
// If a name starts with "*", then the exported bit represents
// whether the pointed to type is exported.
type name struct {
	bytes *byte
}

func (n name) data(off int, whySafe string) *byte {
	return (*byte)(add(unsafe.Pointer(n.bytes), uintptr(off), whySafe))
}

func (n name) isExported() bool {
	return (*n.bytes)&(1<<0) != 0
}

func (n name) hasTag() bool {
	return (*n.bytes)&(1<<1) != 0
}

func (n name) embedded() bool {
	return (*n.bytes)&(1<<3) != 0
}

// readVarint parses a varint as encoded by encoding/binary.
// It returns the number of encoded bytes and the encoded value.
func (n name) readVarint(off int) (int, int) {
	v := 0
	for i := 0; ; i++ {
		x := *n.data(off+i, "read varint")
		v += int(x&0x7f) << (7 * i)
		if x&0x80 == 0 {
			return i + 1, v
		}
	}
}

func (n name) name() string {
	if n.bytes == nil {
		return ""
	}
	i, l := n.readVarint(1)
	return unsafe.String(n.data(1+i, "non-empty string"), l)
}

func (n name) tag() string {
	if !n.hasTag() {
		return ""
	}
	i, l := n.readVarint(1)
	i2, l2 := n.readVarint(1 + i + l)
	return unsafe.String(n.data(1+i+l+i2, "non-empty string"), l2)
}

func pkgPath(n abi.Name) string {
	if n.Bytes == nil || *n.DataChecked(0, "name flag field")&(1<<2) == 0 {
		return ""
	}
	i, l := n.ReadVarint(1)
	off := 1 + i + l
	if n.HasTag() {
		i2, l2 := n.ReadVarint(off)
		off += i2 + l2
	}
	var nameOff int32
	// Note that this field may not be aligned in memory,
	// so we cannot use a direct int32 assignment here.
	copy((*[4]byte)(unsafe.Pointer(&nameOff))[:], (*[4]byte)(unsafe.Pointer(n.DataChecked(off, "name offset field")))[:])
	pkgPathName := name{(*byte)(resolveTypeOff(unsafe.Pointer(n.Bytes), nameOff))}
	return pkgPathName.name()
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

/*
 * The compiler knows the exact layout of all the data structures above.
 * The compiler does not know about the data structures and methods below.
 */

<<<<<<< go/./internal/reflectlite/type.go
const (
	kindDirectIface = 1 << 5
	kindGCProg      = 1 << 6 // Type.gc points to GC program
	kindMask        = (1 << 5) - 1
)

// String returns the name of k.
func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return kindNames[0]
}

var kindNames = []string{
	Invalid:       "invalid",
	Bool:          "bool",
	Int:           "int",
	Int8:          "int8",
	Int16:         "int16",
	Int32:         "int32",
	Int64:         "int64",
	Uint:          "uint",
	Uint8:         "uint8",
	Uint16:        "uint16",
	Uint32:        "uint32",
	Uint64:        "uint64",
	Uintptr:       "uintptr",
	Float32:       "float32",
	Float64:       "float64",
	Complex64:     "complex64",
	Complex128:    "complex128",
	Array:         "array",
	Chan:          "chan",
	Func:          "func",
	Interface:     "interface",
	Map:           "map",
	Ptr:           "ptr",
	Slice:         "slice",
	String:        "string",
	Struct:        "struct",
	UnsafePointer: "unsafe.Pointer",
}

func (t *uncommonType) exportedMethods() []method {
	allm := t.methods
	allExported := true
	for _, m := range allm {
		if m.pkgPath != nil {
			allExported = false
			break
		}
	}
	var methods []method
	if allExported {
		methods = allm
	} else {
		methods = make([]method, 0, len(allm))
		for _, m := range allm {
			if m.pkgPath == nil {
				methods = append(methods, m)
			}
		}
		methods = methods[:len(methods):len(methods)]
	}

	return methods
}

func (t *uncommonType) uncommon() *uncommonType {
	return t
=======
// resolveNameOff resolves a name offset from a base pointer.
// The (*rtype).nameOff method is a convenience wrapper for this function.
// Implemented in the runtime package.
func resolveNameOff(ptrInModule unsafe.Pointer, off int32) unsafe.Pointer

// resolveTypeOff resolves an *rtype offset from a base type.
// The (*rtype).typeOff method is a convenience wrapper for this function.
// Implemented in the runtime package.
func resolveTypeOff(rtype unsafe.Pointer, off int32) unsafe.Pointer

func (t rtype) nameOff(off nameOff) abi.Name {
	return abi.Name{Bytes: (*byte)(resolveNameOff(unsafe.Pointer(t.Type), int32(off)))}
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

<<<<<<< go/./internal/reflectlite/type.go
func (t *uncommonType) PkgPath() string {
	if t == nil || t.pkgPath == nil {
		return ""
	}
	return *t.pkgPath
=======
func (t rtype) typeOff(off typeOff) *abi.Type {
	return (*abi.Type)(resolveTypeOff(unsafe.Pointer(t.Type), int32(off)))
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

<<<<<<< go/./internal/reflectlite/type.go
func (t *uncommonType) Name() string {
	if t == nil || t.name == nil {
		return ""
	}
	return *t.name
=======
func (t rtype) uncommon() *uncommonType {
	return t.Uncommon()
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

<<<<<<< go/./internal/reflectlite/type.go
func (t *rtype) String() string {
	// For gccgo, strip out quoted strings.
	s := *t.string
	var q bool
	r := make([]byte, len(s))
	j := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\t' {
			q = !q
		} else if !q {
			r[j] = s[i]
			j++
		}
=======
func (t rtype) String() string {
	s := t.nameOff(t.Str).Name()
	if t.TFlag&abi.TFlagExtraStar != 0 {
		return s[1:]
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
	}
	return string(r[:j])
}

func (t rtype) common() *abi.Type { return t.Type }

func (t rtype) exportedMethods() []abi.Method {
	ut := t.uncommon()
	if ut == nil {
		return nil
	}
	return ut.ExportedMethods()
}

func (t rtype) NumMethod() int {
	tt := t.Type.InterfaceType()
	if tt != nil {
		return tt.NumMethod()
	}
	return len(t.exportedMethods())
}

<<<<<<< go/./internal/reflectlite/type.go
func (t *rtype) PkgPath() string {
	return t.uncommonType.PkgPath()
=======
func (t rtype) PkgPath() string {
	if t.TFlag&abi.TFlagNamed == 0 {
		return ""
	}
	ut := t.uncommon()
	if ut == nil {
		return ""
	}
	return t.nameOff(ut.PkgPath).Name()
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

<<<<<<< go/./internal/reflectlite/type.go
func (t *rtype) hasName() bool {
	return t.uncommonType != nil && t.uncommonType.name != nil
}

func (t *rtype) Name() string {
	return t.uncommonType.Name()
=======
func (t rtype) Name() string {
	if !t.HasName() {
		return ""
	}
	s := t.String()
	i := len(s) - 1
	sqBrackets := 0
	for i >= 0 && (s[i] != '.' || sqBrackets != 0) {
		switch s[i] {
		case ']':
			sqBrackets++
		case '[':
			sqBrackets--
		}
		i--
	}
	return s[i+1:]
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

func toRType(t *abi.Type) rtype {
	return rtype{t}
}

func elem(t *abi.Type) *abi.Type {
	et := t.Elem()
	if et != nil {
		return et
	}
	panic("reflect: Elem of invalid type " + toRType(t).String())
}

func (t rtype) Elem() Type {
	return toType(elem(t.common()))
}

func (t rtype) In(i int) Type {
	tt := t.Type.FuncType()
	if tt == nil {
		panic("reflect: In of non-func type")
	}
<<<<<<< go/./internal/reflectlite/type.go
	tt := (*funcType)(unsafe.Pointer(t))
	return toType(tt.in[i])
=======
	return toType(tt.InSlice()[i])
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

func (t rtype) Key() Type {
	tt := t.Type.MapType()
	if tt == nil {
		panic("reflect: Key of non-map type")
	}
	return toType(tt.Key)
}

func (t rtype) Len() int {
	tt := t.Type.ArrayType()
	if tt == nil {
		panic("reflect: Len of non-array type")
	}
	return int(tt.Len)
}

func (t rtype) NumField() int {
	tt := t.Type.StructType()
	if tt == nil {
		panic("reflect: NumField of non-struct type")
	}
	return len(tt.Fields)
}

func (t rtype) NumIn() int {
	tt := t.Type.FuncType()
	if tt == nil {
		panic("reflect: NumIn of non-func type")
	}
<<<<<<< go/./internal/reflectlite/type.go
	tt := (*funcType)(unsafe.Pointer(t))
	return len(tt.in)
=======
	return int(tt.InCount)
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

func (t rtype) NumOut() int {
	tt := t.Type.FuncType()
	if tt == nil {
		panic("reflect: NumOut of non-func type")
	}
<<<<<<< go/./internal/reflectlite/type.go
	tt := (*funcType)(unsafe.Pointer(t))
	return len(tt.out)
=======
	return tt.NumOut()
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

func (t rtype) Out(i int) Type {
	tt := t.Type.FuncType()
	if tt == nil {
		panic("reflect: Out of non-func type")
	}
<<<<<<< go/./internal/reflectlite/type.go
	tt := (*funcType)(unsafe.Pointer(t))
	return toType(tt.out[i])
=======
	return toType(tt.OutSlice()[i])
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

// add returns p+x.
//
// The whySafe string is ignored, so that the function still inlines
// as efficiently as p+x, but all call sites should use the string to
// record why the addition is safe, which is to say why the addition
// does not cause x to advance to the very end of p's allocation
// and therefore point incorrectly at the next block in memory.
func add(p unsafe.Pointer, x uintptr, whySafe string) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) + x)
}

// TypeOf returns the reflection Type that represents the dynamic type of i.
// If i is a nil interface value, TypeOf returns nil.
func TypeOf(i any) Type {
	eface := *(*emptyInterface)(unsafe.Pointer(&i))
	return toType(eface.typ)
}

func (t rtype) Implements(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.Implements")
	}
	if u.Kind() != Interface {
		panic("reflect: non-interface type passed to Type.Implements")
	}
	return implements(u.common(), t.common())
}

func (t rtype) AssignableTo(u Type) bool {
	if u == nil {
		panic("reflect: nil type passed to Type.AssignableTo")
	}
	uu := u.common()
	tt := t.common()
	return directlyAssignable(uu, tt) || implements(uu, tt)
}

<<<<<<< go/./internal/reflectlite/type.go
func (t *rtype) Comparable() bool {
	switch t.Kind() {
	case Bool, Int, Int8, Int16, Int32, Int64,
		Uint, Uint8, Uint16, Uint32, Uint64, Uintptr,
		Float32, Float64, Complex64, Complex128,
		Chan, Interface, Ptr, String, UnsafePointer:
		return true

	case Func, Map, Slice:
		return false

	case Array:
		return (*arrayType)(unsafe.Pointer(t)).elem.Comparable()

	case Struct:
		tt := (*structType)(unsafe.Pointer(t))
		for i := range tt.fields {
			if !tt.fields[i].typ.Comparable() {
				return false
			}
		}
		return true

	default:
		panic("reflectlite: impossible")
	}
=======
func (t rtype) Comparable() bool {
	return t.Equal != nil
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
}

// implements reports whether the type V implements the interface type T.
func implements(T, V *abi.Type) bool {
	t := T.InterfaceType()
	if t == nil {
		return false
	}
	if len(t.Methods) == 0 {
		return true
	}
	rT := toRType(T)
	rV := toRType(V)

	// The same algorithm applies in both cases, but the
	// method tables for an interface type and a concrete type
	// are different, so the code is duplicated.
	// In both cases the algorithm is a linear scan over the two
	// lists - T's methods and V's methods - simultaneously.
	// Since method tables are stored in a unique sorted order
	// (alphabetical, with no duplicate method names), the scan
	// through V's methods must hit a match for each of T's
	// methods along the way, or else V does not implement T.
	// This lets us run the scan in overall linear time instead of
	// the quadratic time  a naive search would require.
	// See also ../runtime/iface.go.
	if V.Kind() == Interface {
		v := (*interfaceType)(unsafe.Pointer(V))
		i := 0
<<<<<<< go/./internal/reflectlite/type.go
		for j := 0; j < len(v.methods); j++ {
			tm := &t.methods[i]
			vm := &v.methods[j]
			if *vm.name == *tm.name && (vm.pkgPath == tm.pkgPath || (vm.pkgPath != nil && tm.pkgPath != nil && *vm.pkgPath == *tm.pkgPath)) && rtypeEqual(toType(vm.typ).common(), toType(tm.typ).common()) {
				if i++; i >= len(t.methods) {
=======
		for j := 0; j < len(v.Methods); j++ {
			tm := &t.Methods[i]
			tmName := rT.nameOff(tm.Name)
			vm := &v.Methods[j]
			vmName := rV.nameOff(vm.Name)
			if vmName.Name() == tmName.Name() && rV.typeOff(vm.Typ) == rT.typeOff(tm.Typ) {
				if !tmName.IsExported() {
					tmPkgPath := pkgPath(tmName)
					if tmPkgPath == "" {
						tmPkgPath = t.PkgPath.Name()
					}
					vmPkgPath := pkgPath(vmName)
					if vmPkgPath == "" {
						vmPkgPath = v.PkgPath.Name()
					}
					if tmPkgPath != vmPkgPath {
						continue
					}
				}
				if i++; i >= len(t.Methods) {
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
					return true
				}
			}
		}
		return false
	}

	v := V.Uncommon()
	if v == nil {
		return false
	}
	i := 0
<<<<<<< go/./internal/reflectlite/type.go
	for j := 0; j < len(v.methods); j++ {
		tm := &t.methods[i]
		vm := &v.methods[j]
		if *vm.name == *tm.name && (vm.pkgPath == tm.pkgPath || (vm.pkgPath != nil && tm.pkgPath != nil && *vm.pkgPath == *tm.pkgPath)) && rtypeEqual(toType(vm.mtyp).common(), toType(tm.typ).common()) {
			if i++; i >= len(t.methods) {
=======
	vmethods := v.Methods()
	for j := 0; j < int(v.Mcount); j++ {
		tm := &t.Methods[i]
		tmName := rT.nameOff(tm.Name)
		vm := vmethods[j]
		vmName := rV.nameOff(vm.Name)
		if vmName.Name() == tmName.Name() && rV.typeOff(vm.Mtyp) == rT.typeOff(tm.Typ) {
			if !tmName.IsExported() {
				tmPkgPath := pkgPath(tmName)
				if tmPkgPath == "" {
					tmPkgPath = t.PkgPath.Name()
				}
				vmPkgPath := pkgPath(vmName)
				if vmPkgPath == "" {
					vmPkgPath = rV.nameOff(v.PkgPath).Name()
				}
				if tmPkgPath != vmPkgPath {
					continue
				}
			}
			if i++; i >= len(t.Methods) {
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
				return true
			}
		}
	}
	return false
}

// directlyAssignable reports whether a value x of type V can be directly
// assigned (using memmove) to a value of type T.
// https://golang.org/doc/go_spec.html#Assignability
// Ignoring the interface rules (implemented elsewhere)
// and the ideal constant rules (no ideal constants at run time).
func directlyAssignable(T, V *abi.Type) bool {
	// x's type V is identical to T?
	if rtypeEqual(T, V) {
		return true
	}

	// Otherwise at least one of T and V must not be defined
	// and they must have the same kind.
	if T.HasName() && V.HasName() || T.Kind() != V.Kind() {
		return false
	}

	// x's type T and V must  have identical underlying types.
	return haveIdenticalUnderlyingType(T, V, true)
}

func haveIdenticalType(T, V *abi.Type, cmpTags bool) bool {
	if cmpTags {
		return T == V
	}

	if toRType(T).Name() != toRType(V).Name() || T.Kind() != V.Kind() {
		return false
	}

	return haveIdenticalUnderlyingType(T, V, false)
}

<<<<<<< go/./internal/reflectlite/type.go
func haveIdenticalUnderlyingType(T, V *rtype, cmpTags bool) bool {
	if rtypeEqual(T, V) {
=======
func haveIdenticalUnderlyingType(T, V *abi.Type, cmpTags bool) bool {
	if T == V {
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
		return true
	}

	kind := T.Kind()
	if kind != V.Kind() {
		return false
	}

	// Non-composite types of equal kind have same underlying type
	// (the predefined instance of the type).
	if abi.Bool <= kind && kind <= abi.Complex128 || kind == abi.String || kind == abi.UnsafePointer {
		return true
	}

	// Composite types.
	switch kind {
	case abi.Array:
		return T.Len() == V.Len() && haveIdenticalType(T.Elem(), V.Elem(), cmpTags)

	case abi.Chan:
		// Special case:
		// x is a bidirectional channel value, T is a channel type,
		// and x's type V and T have identical element types.
		if V.ChanDir() == abi.BothDir && haveIdenticalType(T.Elem(), V.Elem(), cmpTags) {
			return true
		}

		// Otherwise continue test for identical underlying type.
		return V.ChanDir() == T.ChanDir() && haveIdenticalType(T.Elem(), V.Elem(), cmpTags)

	case abi.Func:
		t := (*funcType)(unsafe.Pointer(T))
		v := (*funcType)(unsafe.Pointer(V))
<<<<<<< go/./internal/reflectlite/type.go
		if t.dotdotdot != v.dotdotdot || len(t.in) != len(v.in) || len(t.out) != len(v.out) {
=======
		if t.OutCount != v.OutCount || t.InCount != v.InCount {
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
			return false
		}
		for i, typ := range t.in {
			if !haveIdenticalType(typ, v.in[i], cmpTags) {
				return false
			}
		}
		for i, typ := range t.out {
			if !haveIdenticalType(typ, v.out[i], cmpTags) {
				return false
			}
		}
		return true

	case Interface:
		t := (*interfaceType)(unsafe.Pointer(T))
		v := (*interfaceType)(unsafe.Pointer(V))
		if len(t.Methods) == 0 && len(v.Methods) == 0 {
			return true
		}
		// Might have the same methods but still
		// need a run time conversion.
		return false

	case abi.Map:
		return haveIdenticalType(T.Key(), V.Key(), cmpTags) && haveIdenticalType(T.Elem(), V.Elem(), cmpTags)

	case Ptr, abi.Slice:
		return haveIdenticalType(T.Elem(), V.Elem(), cmpTags)

	case abi.Struct:
		t := (*structType)(unsafe.Pointer(T))
		v := (*structType)(unsafe.Pointer(V))
		if len(t.Fields) != len(v.Fields) {
			return false
		}
<<<<<<< go/./internal/reflectlite/type.go
		for i := range t.fields {
			tf := &t.fields[i]
			vf := &v.fields[i]
			if tf.name != vf.name && (tf.name == nil || vf.name == nil || *tf.name != *vf.name) {
=======
		if t.PkgPath.Name() != v.PkgPath.Name() {
			return false
		}
		for i := range t.Fields {
			tf := &t.Fields[i]
			vf := &v.Fields[i]
			if tf.Name.Name() != vf.Name.Name() {
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
				return false
			}
<<<<<<< go/./internal/reflectlite/type.go
			if tf.pkgPath != vf.pkgPath && (tf.pkgPath == nil || vf.pkgPath == nil || *tf.pkgPath != *vf.pkgPath) {
=======
			if !haveIdenticalType(tf.Typ, vf.Typ, cmpTags) {
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
				return false
			}
<<<<<<< go/./internal/reflectlite/type.go
			if !haveIdenticalType(tf.typ, vf.typ, cmpTags) {
=======
			if cmpTags && tf.Name.Tag() != vf.Name.Tag() {
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
				return false
			}
<<<<<<< go/./internal/reflectlite/type.go
			if cmpTags && tf.tag != vf.tag && (tf.tag == nil || vf.tag == nil || *tf.tag != *vf.tag) {
=======
			if tf.Offset != vf.Offset {
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
				return false
			}
<<<<<<< go/./internal/reflectlite/type.go
			if tf.offsetEmbed != vf.offsetEmbed {
=======
			if tf.Embedded() != vf.Embedded() {
>>>>>>> /tmp/go121/src/./internal/reflectlite/type.go
				return false
			}
		}
		return true
	}

	return false
}

// toType converts from a *rtype to a Type that can be returned
// to the client of package reflect. In gc, the only concern is that
// a nil *rtype must be replaced by a nil Type, but in gccgo this
// function takes care of ensuring that multiple *rtype for the same
// type are coalesced into a single Type.
func toType(t *abi.Type) Type {
	if t == nil {
		return nil
	}
	return toRType(t)
}

// ifaceIndir reports whether t is stored indirectly in an interface value.
func ifaceIndir(t *abi.Type) bool {
	return t.Kind_&abi.KindDirectIface == 0
}
