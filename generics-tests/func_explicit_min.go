package main

import "fmt"

func Min[T int | float64](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func main() {
	fmt.Println(Min[int](3, 5))
	fmt.Println(Min[float64](2.5, 1.5))
}
