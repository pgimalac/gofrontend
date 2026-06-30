package lib

type Aggregator[N int64 | float64] interface {
	Aggregate(v N)
	Sum() N
}

type valueMap[N int64 | float64] struct {
	values map[int]N
}

func newValueMap[N int64 | float64]() *valueMap[N] {
	return &valueMap[N]{values: make(map[int]N)}
}

func (s *valueMap[N]) Aggregate(v N) { s.values[0] += v }

// deltaSum embeds a POINTER to a generic instance and is built with a
// composite literal that names the embedded field; Sum reads a field
// promoted through the embedded pointer.
type deltaSum[N int64 | float64] struct {
	*valueMap[N]
	monotonic bool
}

func (s *deltaSum[N]) Sum() N { return s.values[0] }

func New[N int64 | float64]() Aggregator[N] {
	return &deltaSum[N]{valueMap: newValueMap[N](), monotonic: true}
}
