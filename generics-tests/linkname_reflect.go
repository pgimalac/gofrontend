package main

// Regression test for the gc-style //go:linkname translation table in the
// frontend (Gogo::add_linkname).  Reproduces the exact pattern used by
// github.com/modern-go/reflect2 -- pulled in transitively by json-iterator,
// prometheus/client_golang and (hence) the OpenTelemetry collector.  Those
// packages reference the standard-library reflect internals by their gc
// symbol names (reflect.unsafe_New, reflect.unsafe_NewArray), which gccgo
// mangles differently (reflect.unsafe__New).  Without the translation the
// program fails to link with "undefined reference to reflect.unsafe_New".

import (
	"fmt"
	"unsafe"
)

//go:linkname unsafe_New reflect.unsafe_New
func unsafe_New(rtype unsafe.Pointer) unsafe.Pointer

//go:linkname unsafe_NewArray reflect.unsafe_NewArray
func unsafe_NewArray(rtype unsafe.Pointer, length int) unsafe.Pointer

type eface struct {
	typ unsafe.Pointer
	val unsafe.Pointer
}

// rtypeOf extracts the runtime type descriptor pointer from an interface,
// the same way reflect2 obtains the *rtype it passes to unsafe_New.
func rtypeOf(v interface{}) unsafe.Pointer {
	return (*eface)(unsafe.Pointer(&v)).typ
}

type T struct {
	A int
	B string
}

func main() {
	p := (*T)(unsafe_New(rtypeOf(T{})))
	p.A = 42
	p.B = "hello"
	fmt.Printf("new: %+v\n", *p)

	arr := unsafe_NewArray(rtypeOf(int(0)), 3)
	s := (*[3]int)(arr)
	s[0], s[1], s[2] = 10, 20, 30
	fmt.Printf("newarray: %v\n", *s)
}
