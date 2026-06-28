package main

import "fmt"

type Box[T any] struct{ V T }

func Wrap[T any](v T) Box[T]   { return Box[T]{V: v} }
func (b Box[T]) Get() T        { return b.V }
func (b Box[T]) Map(f func(T) T) Box[T] { return Box[T]{V: f(b.V)} }

func main() {
	// Inference produces a generic-type instance, then a method is
	// called on it in the same expression.
	fmt.Println(Wrap(7).Get())
	fmt.Println(Wrap("hi").Get())
	fmt.Println(Wrap(3).Map(func(x int) int { return x * 10 }).Get())
}
