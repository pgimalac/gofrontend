package main

import "fmt"

type Box[T any] struct{ V T }

func Wrap[T any](v T) Box[T]              { return Box[T]{V: v} }
func (b Box[T]) Get() T                   { return b.V }
func WrapTwice[T any](v T) Box[Box[T]]    { return Wrap(Wrap(v)) }

func main() {
	// Nested inference: the inner call's result type feeds the outer
	// call's inference, and the same instantiated type is reached through
	// two different spellings.
	fmt.Println(Wrap(Wrap(7)).Get().Get())
	b := WrapTwice("hi")
	fmt.Println(b.Get().Get())
}
