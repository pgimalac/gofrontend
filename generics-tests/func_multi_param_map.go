package main

import "fmt"

func Map[T any, U any](s []T, f func(T) U) []U {
	r := make([]U, len(s))
	for i, v := range s {
		r[i] = f(v)
	}
	return r
}

func main() {
	xs := []int{1, 2, 3}
	ys := Map[int, string](xs, func(x int) string { return fmt.Sprintf("<%d>", x) })
	fmt.Println(ys)
}
