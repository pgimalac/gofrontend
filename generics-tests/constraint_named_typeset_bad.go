// Negative test: a struct does not satisfy a named type-set constraint.
package main

type Number interface{ ~int | ~float64 }

func Sum[T Number](xs []T) T {
	var t T
	for _, x := range xs {
		t += x
	}
	return t
}

type Pt struct{ X int }

func main() {
	_ = Sum([]Pt{{1}, {2}})
}
