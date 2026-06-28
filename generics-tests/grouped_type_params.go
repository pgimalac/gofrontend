package main

import "fmt"

func Pair[T, U any](a T, b U) (T, U) { return a, b }

func main() {
	x, y := Pair[int, string](1, "two")
	fmt.Println(x, y)
	a, b := Pair(3.5, true) // inferred
	fmt.Println(a, b)
}
