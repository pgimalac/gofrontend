package main

import "fmt"

type Box[T any] struct{ V T }
type Pair[A, B any] struct {
	First  A
	Second B
}

// Type parameters appear inside a named generic type used as a
// parameter; they must be inferred from the argument's type.
func Unwrap[T any](b Box[T]) T            { return b.V }
func Swap[A, B any](p Pair[A, B]) Pair[B, A] { return Pair[B, A]{First: p.Second, Second: p.First} }

func main() {
	fmt.Println(Unwrap(Box[string]{V: "hello"}))
	fmt.Println(Unwrap(Box[int]{V: 99}))
	s := Swap(Pair[int, string]{First: 1, Second: "one"})
	fmt.Println(s.First, s.Second)
}
