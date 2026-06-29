package main

import "fmt"

func main() {
	// Generic functions used with explicit (and partial) type arguments
	// before they are declared in the file.
	fmt.Println(Id[int](5))
	a, b := Pair[int, string](1, "x")
	fmt.Println(a, b)
	c, d := Pair[int](2, "y")
	fmt.Println(c, d)
}

func Id[T any](v T) T                 { return v }
func Pair[A, B any](a A, b B) (A, B)  { return a, b }
