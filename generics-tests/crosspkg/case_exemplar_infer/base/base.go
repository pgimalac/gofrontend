package base

// This package mirrors the shape of go.opentelemetry.io/otel's internal
// exemplar package that exposed three cross-package generics bugs in the
// gccgo frontend:
//
//  1. A generic function (NewValue) whose type argument is inferred from a
//     type-parameter-typed argument, called from inside another package's
//     generic method body during token-replay instantiation.
//  2. A composite literal of this package's struct type with UNEXPORTED
//     fields, appearing in a generic template body re-parsed in the
//     importing package.
//  3. A generic function (Drop) used as a *value* (not called) whose type
//     argument must be inferred from the context function type.

// Value is a struct with unexported fields, like exemplar.Value.
type Value struct {
	t   uint8
	val int64
}

func (v Value) T() uint8   { return v.t }
func (v Value) Val() int64 { return v.val }

// NewValue infers N from its type-parameter-typed argument, and returns a
// composite literal of Value that assigns to its unexported fields.
func NewValue[N int64 | float64](value N) Value {
	switch v := any(value).(type) {
	case int64:
		return Value{t: 1, val: v}
	case float64:
		return Value{t: 2, val: int64(v)}
	}
	return Value{}
}

// Reservoir takes a (non-generic) Value.
type Reservoir interface {
	Offer(t int, val Value)
}

type simpleRes struct{ Last Value }

func (s *simpleRes) Offer(t int, val Value) { s.Last = val }

func NewReservoir() Reservoir { return &simpleRes{} }

// FilteredReservoirLike is a generic interface, like exemplar.FilteredReservoir.
type FilteredReservoirLike[N int64 | float64] interface {
	Offer2(val N) int64
}

type dropRes[N int64 | float64] struct{}

func (r dropRes[N]) Offer2(val N) int64 { return 0 }

// Drop is a generic function used as a value in the importer; its type
// argument is inferred from the context function type.
func Drop[N int64 | float64]() FilteredReservoirLike[N] { return dropRes[N]{} }
