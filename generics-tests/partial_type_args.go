package main

import "fmt"

func Pair[A, B any](a A, b B) (A, B) { return a, b }

func Map[T, U any](s []T, f func(T) U) []U {
	r := make([]U, len(s))
	for i, v := range s {
		r[i] = f(v)
	}
	return r
}

func main() {
	// Provide the first type argument explicitly, infer the rest.
	x, y := Pair[int](1, "s")
	fmt.Println(x, y)
	r := Map[int]([]int{1, 2, 3}, func(v int) int { return v + 1 })
	fmt.Println(r)
}
