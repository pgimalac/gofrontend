package main

import (
	"fmt"
	"xtest/case_types/lib"
)

func main() {
	var s lib.Stack[string]
	s.Push("x")
	s.Push("y")
	fmt.Println(s.Len())
	v, ok := s.Pop()
	fmt.Println(v, ok)
	p := lib.Pair[int, string]{First: 1, Second: "one"}
	fmt.Println(p.First, p.Second)
	q := lib.MakePair(true, 3.5)
	fmt.Println(q.First, q.Second)
}
