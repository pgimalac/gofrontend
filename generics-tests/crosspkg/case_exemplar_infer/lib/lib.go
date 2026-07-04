package lib

import "xtest/case_exemplar_infer/base"

// FilteredReservoir mirrors exemplar.FilteredReservoir: a generic interface.
type FilteredReservoir[N int64 | float64] interface {
	Offer(t int, val N)
}

// filteredReservoir implements it, wrapping a base.Reservoir.
type filteredReservoir[N int64 | float64] struct {
	reservoir base.Reservoir
}

func NewFilteredReservoir[N int64 | float64](r base.Reservoir) FilteredReservoir[N] {
	return &filteredReservoir[N]{reservoir: r}
}

// Offer nests base.NewValue(val) -- with val of type parameter N -- as an
// argument to the (non-generic) interface method reservoir.Offer.  When this
// method is instantiated for a concrete N during the importing package's
// compilation, base.NewValue's type argument must be inferred as that N.
func (f *filteredReservoir[N]) Offer(t int, val N) {
	f.reservoir.Offer(t, base.NewValue(val))
}
