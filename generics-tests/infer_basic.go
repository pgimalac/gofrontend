package main

import "fmt"

func Id[T any](x T) T { return x }

func Min[T int | float64](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func Map[T any, U any](s []T, f func(T) U) []U {
	r := make([]U, 0, len(s))
	for _, v := range s {
		r = append(r, f(v))
	}
	return r
}

func main() {
	fmt.Println(Id(42))            // T=int inferred
	fmt.Println(Id("hello"))       // T=string inferred
	fmt.Println(Min(3, 5))         // T=int
	fmt.Println(Min(2.5, 1.5))     // T=float64
	xs := []int{1, 2, 3}
	fmt.Println(Map(xs, func(x int) int { return x * 10 })) // T=int,U=int from []T and func(T)U
}
