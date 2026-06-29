package main

import "fmt"

type Ordered interface {
	~int | ~int64 | ~float64 | ~string
}
type Number interface {
	~int | ~float64
}
type MyInt int

func Max[T Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func Sum[T Number](xs []T) T {
	var t T
	for _, x := range xs {
		t += x
	}
	return t
}

func main() {
	fmt.Println(Max(3, 5))
	fmt.Println(Max("a", "b"))
	fmt.Println(Max(MyInt(2), MyInt(9)))
	fmt.Println(Sum([]int{1, 2, 3}))
	fmt.Println(Sum([]float64{1.5, 2.5}))
}
