// Constraint type inference for a type parameter whose constraint mixes a
// structural term with an embedded (method-set) interface, e.g.
// "P interface{ *T; Unmarsh }".  Once T is inferred from the argument, P must
// be inferred from its constraint's core type (*T); the embedded interface
// "Unmarsh" contributes only a method set and must not be mistaken for a
// second structural term (which would defeat inference).  Mirrors
// github.com/google/go-tpm/tpm2 New2B[T Marshallable, P interface{ *T;
// Unmarshallable }].
package main

import "fmt"

type Marsh interface{ mm() }
type Unmarsh interface{ uu() }

type Box[T Marsh, P interface {
	*T
	Unmarsh
}] struct{ p P }

func New[T Marsh, P interface {
	*T
	Unmarsh
}](t T) Box[T, P] {
	return Box[T, P]{p: &t}
}

type S struct{ v int }

func (s S) mm()  {}
func (s *S) uu() {}

func main() {
	var b Box[S, *S]
	b = New(S{7}) // P inferred as *S from the constraint core type
	fmt.Println(b.p.v)
}
