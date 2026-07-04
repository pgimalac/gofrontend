// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reflect

<<<<<<< go/./reflect/export_test.go
=======
import (
	"internal/abi"
	"internal/goarch"
	"sync"
	"unsafe"
)

>>>>>>> /tmp/go121/src/./reflect/export_test.go
// MakeRO returns a copy of v with the read-only flag set.
func MakeRO(v Value) Value {
	v.flag |= flagStickyRO
	return v
}

// IsRO reports whether v's read-only flag is set.
func IsRO(v Value) bool {
	return v.flag&flagStickyRO != 0
}

var CallGC = &callGC

// FuncLayout calls funcLayout and returns a subset of the results for testing.
//
// Bitmaps like stack, gc, inReg, and outReg are expanded such that each bit
// takes up one byte, so that writing out test cases is a little clearer.
// If ptrs is false, gc will be nil.
func FuncLayout(t Type, rcvr Type) (frametype Type, argSize, retOffset uintptr, stack, gc, inReg, outReg []byte, ptrs bool) {
<<<<<<< go/./reflect/export_test.go
=======
	var ft *abi.Type
	var abid abiDesc
	if rcvr != nil {
		ft, _, abid = funcLayout((*funcType)(unsafe.Pointer(t.common())), rcvr.common())
	} else {
		ft, _, abid = funcLayout((*funcType)(unsafe.Pointer(t.(*rtype))), nil)
	}
	// Extract size information.
	argSize = abid.stackCallArgsSize
	retOffset = abid.retOffset
	frametype = toType(ft)

	// Expand stack pointer bitmap into byte-map.
	for i := uint32(0); i < abid.stackPtrs.n; i++ {
		stack = append(stack, abid.stackPtrs.data[i/8]>>(i%8)&1)
	}

	// Expand register pointer bitmaps into byte-maps.
	bool2byte := func(b bool) byte {
		if b {
			return 1
		}
		return 0
	}
	for i := 0; i < intArgRegs; i++ {
		inReg = append(inReg, bool2byte(abid.inRegPtrs.Get(i)))
		outReg = append(outReg, bool2byte(abid.outRegPtrs.Get(i)))
	}
	if ft.Kind_&kindGCProg != 0 {
		panic("can't handle gc programs")
	}

	// Expand frame type's GC bitmap into byte-map.
	ptrs = ft.PtrBytes != 0
	if ptrs {
		nptrs := ft.PtrBytes / goarch.PtrSize
		gcdata := ft.GcSlice(0, (nptrs+7)/8)
		for i := uintptr(0); i < nptrs; i++ {
			gc = append(gc, gcdata[i/8]>>(i%8)&1)
		}
	}
>>>>>>> /tmp/go121/src/./reflect/export_test.go
	return
}

func TypeLinks() []string {
	return nil
}

var GCBits = gcbits

// Will be provided by runtime eventually.
func gcbits(interface{}) []byte {
	return nil
}

func MapBucketOf(x, y Type) Type {
<<<<<<< go/./reflect/export_test.go
	return nil
=======
	return toType(bucketOf(x.common(), y.common()))
>>>>>>> /tmp/go121/src/./reflect/export_test.go
}

func CachedBucketOf(m Type) Type {
<<<<<<< go/./reflect/export_test.go
	return nil
=======
	t := m.(*rtype)
	if Kind(t.t.Kind_&kindMask) != Map {
		panic("not map")
	}
	tt := (*mapType)(unsafe.Pointer(t))
	return toType(tt.Bucket)
>>>>>>> /tmp/go121/src/./reflect/export_test.go
}

type EmbedWithUnexpMeth struct{}

func (EmbedWithUnexpMeth) f() {}

type pinUnexpMeth interface {
	f()
}

var pinUnexpMethI = pinUnexpMeth(EmbedWithUnexpMeth{})

/*
func FirstMethodNameBytes(t Type) *byte {
	_ = pinUnexpMethI

	ut := t.uncommon()
	if ut == nil {
		panic("type has no methods")
	}
	m := ut.Methods()[0]
	mname := t.(*rtype).nameOff(m.Name)
	if *mname.DataChecked(0, "name flag field")&(1<<2) == 0 {
		panic("method name does not have pkgPath *string")
	}
	return mname.Bytes
}
*/

type OtherPkgFields struct {
	OtherExported   int
	otherUnexported int
}

func IsExported(t Type) bool {
<<<<<<< go/./reflect/export_test.go
	return t.PkgPath() == ""
=======
	typ := t.(*rtype)
	n := typ.nameOff(typ.t.Str)
	return n.IsExported()
>>>>>>> /tmp/go121/src/./reflect/export_test.go
}

/*
func ResolveReflectName(s string) {
	resolveReflectName(newName(s, "", false, false))
}
*/

type Buffer struct {
	buf []byte
}

var MethodValueCallCodePtr = methodValueCallCodePtr
