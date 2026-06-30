package main

import "fmt"

// Observer is satisfied by *impl below.
type Observer[N int64 | float64] interface{ Observe(v N) }

type impl[N int64 | float64] struct{ total N }

// These interface-satisfaction assertions instantiate impl[...] BEFORE the
// method Observe is declared (a common pattern in real code).  The instance's
// method set must still include methods declared later in the package.
var _ Observer[float64] = &impl[float64]{}
var _ Observer[int64] = &impl[int64]{}

func (i *impl[N]) Observe(v N) { i.total += v }

func main() {
	var o Observer[int64] = &impl[int64]{}
	o.Observe(5)
	o.Observe(7)
	fmt.Println(o.(*impl[int64]).total)
}
