package main

import "fmt"

func Map[T any, U any](s []T, f func(T) U) []U {
	r := make([]U, 0, len(s))
	for _, v := range s {
		r = append(r, f(v))
	}
	return r
}

func Twice[T any](x T) []T {
	return []T{x, x}
}

func main() {
	a := Map[int, int]([]int{1, 2, 3}, func(x int) int { return x * x })
	fmt.Println(a)
	fmt.Println(Twice[string]("hi"))
}
