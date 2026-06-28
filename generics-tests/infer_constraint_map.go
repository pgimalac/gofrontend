package main

import "fmt"

type Number interface{ ~int | ~float64 }

func Sum[T Number](xs []T) T {
	var total T
	for _, x := range xs {
		total += x
	}
	return total
}

func Keys[K comparable, V any](m map[K]V) []K {
	r := make([]K, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	return r
}

func main() {
	fmt.Println(Sum([]int{1, 2, 3, 4}))       // infer T=int
	fmt.Println(Sum([]float64{1.5, 2.5}))     // infer T=float64
	m := map[string]int{"a": 1}
	fmt.Println(Keys(m))                       // infer K=string,V=int from map[K]V
}
