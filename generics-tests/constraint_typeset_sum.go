package main

import "fmt"

type Number interface {
	~int | ~float64
}

func Sum[T Number](xs []T) T {
	var total T
	for _, x := range xs {
		total += x
	}
	return total
}

func main() {
	fmt.Println(Sum[int]([]int{1, 2, 3, 4}))
	fmt.Println(Sum[float64]([]float64{1.5, 2.5}))
}
