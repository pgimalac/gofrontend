// Function-local generic type declarations, including inside a generic
// function where the local type captures the enclosing type parameter.
// Each enclosing instantiation must produce a distinct nominal type.
package main

import (
	"fmt"
	"reflect"
)

// Local generic type inside a NON-generic function.
func nonGeneric() {
	type Stack[T any] struct{ items []T }
	var s Stack[int]
	s.items = append(s.items, 1, 2, 3)
	fmt.Println("stack:", s.items)
}

// Local generic type inside a GENERIC function, capturing the enclosing
// type parameter A in a field.
func capturing[A any](a A) {
	type Box[B any] struct {
		a A
		b B
	}
	x := Box[int]{a: a, b: 7}
	fmt.Println("box:", x.a, x.b)
}

// Enclosing type parameter used as the local type's own argument.
func ownArg[A any](a A) {
	type Box[B any] struct{ v B }
	var x Box[A]
	x.v = a
	fmt.Println("ownarg:", x.v)
}

// Recursive local generic type.
func recursive[A any]() {
	type Node[T any] struct {
		val  T
		next *Node[T]
	}
	n2 := &Node[int]{val: 2}
	n1 := &Node[int]{val: 1, next: n2}
	fmt.Print("list:")
	for n := n1; n != nil; n = n.next {
		fmt.Print(" ", n.val)
	}
	fmt.Println()
}

// Distinct nominal identity: F[string].Box[int] and F[float64].Box[int]
// must be different reflect.Types.
func identity[A any]() interface{} {
	type Box[B any] struct {
		a A
		b B
	}
	var x Box[int]
	return x
}

func main() {
	nonGeneric()
	capturing("s")
	capturing(1.5)
	ownArg(11)
	ownArg("hi")
	recursive[string]()

	t1 := reflect.TypeOf(identity[string]())
	t2 := reflect.TypeOf(identity[float64]())
	t3 := reflect.TypeOf(identity[string]())
	fmt.Println("identity distinct:", t1 != t2)
	fmt.Println("identity same:", t1 == t3)
}
