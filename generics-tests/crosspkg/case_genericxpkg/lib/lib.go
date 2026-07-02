package lib

type Box[T any] struct{ V T }

// Filler is a generic interface with an unexported method.  When Run below is
// instantiated in the importing package, the interface (and its unexported
// method "filtered") is re-parsed; the method name must be packed with THIS
// package's pkgpath (not the importer's) to match mapFiller's method.
type Filler[N int64 | float64] interface {
	Fill(N)
	filtered(N)
}

type mapFiller[N int64 | float64] struct{ Sum N }

func (m *mapFiller[N]) Fill(v N)     { m.Sum += v }
func (m *mapFiller[N]) filtered(v N) { m.Sum += v * 2 }

// Run takes a generic-type value as an *unnamed* function parameter
// ("func(Box[N]) N"); recognizing "Box[...]" as a generic instantiation while
// re-parsing this template in the importer requires resolving the generic type
// against this (defining) package.  It also type-asserts to the generic
// interface with the unexported method.
func Run[N int64 | float64](f func(Box[N]) N, b Box[N]) N {
	var x any = &mapFiller[N]{}
	if fa, ok := x.(Filler[N]); ok {
		fa.filtered(b.V)
	}
	return f(b)
}
