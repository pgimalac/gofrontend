package main

import "fmt"

type Pair[A any, B any] struct {
	First  A
	Second B
}

func MakePair[A any, B any](a A, b B) Pair[A, B] {
	return Pair[A, B]{First: a, Second: b}
}

func main() {
	p := MakePair[int, string](42, "hi")
	fmt.Println(p.First, p.Second)
	var q Pair[string, int]
	q.First = "x"
	q.Second = 7
	fmt.Println(q.First, q.Second)
}
