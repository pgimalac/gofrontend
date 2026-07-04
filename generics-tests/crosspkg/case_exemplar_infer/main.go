package main

import (
	"fmt"

	"xtest/case_exemplar_infer/base"
	"xtest/case_exemplar_infer/lib"
)

// reservoirFunc mirrors sdk/metric.reservoirFunc: a generic function that
// instantiates lib.NewFilteredReservoir[N] (which pulls in the whole chain
// above), and also returns base.Drop as a bare value inferred from context.
func reservoirFunc[N int64 | float64](drop bool) func() lib.FilteredReservoir[N] {
	return func() lib.FilteredReservoir[N] {
		return lib.NewFilteredReservoir[N](base.NewReservoir())
	}
}

func dropFunc[N int64 | float64]() func() base.FilteredReservoirLike[N] {
	return base.Drop
}

func main() {
	fi := reservoirFunc[int64](false)
	fi().Offer(1, 42)

	ff := reservoirFunc[float64](false)
	ff().Offer(2, 3)

	di := dropFunc[int64]()
	fmt.Println(di().Offer2(7))

	df := dropFunc[float64]()
	fmt.Println(df().Offer2(3.5))

	fmt.Println("ok")
}
