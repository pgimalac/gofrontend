package lib

import "xtest/case_genericxpkg_import/base"

// Box is generic and has a field whose element type comes from base.
type Box[N int64 | float64] struct {
	Items []base.KV
	N     N
}

type Agg interface{ isAgg() }

func (Box[N]) isAgg() {}

func New[N int64 | float64](n N) Box[N] { return Box[N]{N: n} }

// KVof returns a base.KV element without the caller importing base.
func KVof(k string, v int) any { return base.KV{Key: k, Value: v} }
