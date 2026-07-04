package lib

import "xtest/case_infer_xpkg_instance/base"

// An unexported generic helper.  It is used below with its type argument
// *inferred* (not written out), which is where the exported spelling of the
// inferred type argument matters.
func reset[T any](s []T, length, capacity int) []T {
	if cap(s) < capacity {
		return make([]T, length, capacity)
	}
	return s[:length]
}

// A generic type whose field element type is an imported generic instance
// parameterized by the local type parameter N (base.DataPoint[N]).
type Holder[N int64 | float64] struct {
	pts []base.DataPoint[N]
}

// A method that calls the generic helper "reset" with h.pts, so its type
// argument T is inferred to be base.DataPoint[N].
func (h *Holder[N]) Grow(n int) {
	h.pts = reset(h.pts, n, n)
	for i := 0; i < n; i++ {
		h.pts[i] = base.DataPoint[N]{Value: N(i)}
	}
}

func (h *Holder[N]) Sum() N {
	var total N
	for _, p := range h.pts {
		total += p.Value
	}
	return total
}

func New[N int64 | float64]() *Holder[N] { return &Holder[N]{} }
