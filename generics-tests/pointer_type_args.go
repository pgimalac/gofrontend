package main

import "fmt"

func Deref[T any](ps []*T) []T {
	out := make([]T, 0, len(ps))
	for _, p := range ps {
		out = append(out, *p)
	}
	return out
}

func First[T any](xs []T) T { return xs[0] }

func main() {
	a, b, c := 1, 2, 3
	fmt.Println(Deref([]*int{&a, &b, &c}))   // infer T=int from []*T
	x := 42
	fmt.Println(First[*int]([]*int{&x}) == &x) // explicit pointer type arg
}
