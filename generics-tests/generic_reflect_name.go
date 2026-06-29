// Generic type instances must reflect with their spec name "Base[args]",
// not the internal mangled instance name.
package main

import (
	"fmt"
	"reflect"
)

type Pair[A any, B any] struct {
	a A
	b B
}

type myInt int

func main() {
	fmt.Println(reflect.TypeOf(Pair[int, string]{}).String())
	fmt.Printf("%T\n", Pair[myInt, *int]{})
	// A type declared after its use in a composite literal.
	fmt.Printf("%T\n", Box[int]{})
}

type Box[T any] struct{ v T }
