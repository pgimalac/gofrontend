package main

import (
	"fmt"
	"xtest/case_funcs/lib"
)

func main() {
	fmt.Println(lib.Max(3, 5))
	fmt.Println(lib.Max("a", "b"))
	fmt.Println(lib.Max[float64](2.5, 1.5))
	xs := []int{1, 2, 3}
	ys := lib.Map(xs, func(v int) int { return v * v })
	fmt.Println(ys)
}
