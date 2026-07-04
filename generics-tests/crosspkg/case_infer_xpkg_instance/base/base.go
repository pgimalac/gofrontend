package base

// A generic named type defined in another package.  When "lib" uses
// base.DataPoint[N] as an element type and passes such a slice to a generic
// helper (see lib.reset), N is inferred to be base.DataPoint[<concrete>].
// The inferred type argument must be spelled with its "base." qualifier in
// lib's export data, or re-parsing the helper instance in "main" fails.
type DataPoint[N int64 | float64] struct {
	Value N
	Attr  string
}
