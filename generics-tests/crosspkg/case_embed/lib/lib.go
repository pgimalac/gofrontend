package lib

// Aggregator is satisfied by the unexported instances below, including via a
// method promoted from an embedded generic field.
type Aggregator[N int64 | float64] interface {
	Aggregate(v N)
	Sum() N
}

type valueMap[N int64 | float64] struct {
	total N
}

func (m *valueMap[N]) Aggregate(v N) { m.total += v }

type sum[N int64 | float64] struct {
	valueMap[N] // embedded generic type; promotes Aggregate
	count       int
}

func (s *sum[N]) Sum() N { return s.total }

// New returns an unexported instance through the exported interface; the
// instance's method set mixes a promoted method (Aggregate, from the
// embedded valueMap) and an own method (Sum).
func New[N int64 | float64]() Aggregator[N] { return &sum[N]{} }
