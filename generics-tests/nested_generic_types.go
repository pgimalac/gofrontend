package main

import "fmt"

type Pair[A any, B any] struct{ First A; Second B }
type Box[T any] struct{ val T }

func main() {
	b := Box[Pair[int, string]]{val: Pair[int, string]{First: 7, Second: "hi"}}
	fmt.Println(b.val.First, b.val.Second)
	xs := []Pair[int, int]{{1, 2}, {3, 4}}
	for _, p := range xs {
		fmt.Println(p.First + p.Second)
	}
}
