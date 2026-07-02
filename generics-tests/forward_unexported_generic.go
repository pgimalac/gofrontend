package main

import "fmt"

// Regression: forward reference to an UNEXPORTED generic type (used before
// its declaration), a generic type as an embedded struct field, and an
// F-bounded constraint (constraint is a generic instantiation of the same
// type parameter) -- all valid Go 1.18 generics, as used by
// crypto/elliptic's nistCurve in Go 1.19.

var b1 = box[int]{v: 7}          // forward ref to unexported box, before decl

type wrapBox struct {            // embedded generic field, box declared later
	box[string]
}

type point[T any] interface{ Set(T) T }

type curve[P point[P]] struct{ name string } // F-bounded

type impl struct{ x int }

func (p *impl) Set(q *impl) *impl { p.x = q.x; return p }

var c1 = &curve[*impl]{name: "c"} // forward-ref F-bounded before curve? no, after

type box[T any] struct{ v T }

func main() {
	w := wrapBox{box[string]{v: "hi"}}
	fmt.Println(b1.v, w.v, c1.name)
}
