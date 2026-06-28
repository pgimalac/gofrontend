package main

import "fmt"

type Stack[T any] struct {
	items []T
}

func (s *Stack[T]) Push(x T) {
	s.items = append(s.items, x)
}

func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	x := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return x, true
}

func (s *Stack[T]) Len() int {
	return len(s.items)
}

// Method with a different receiver type-param name than the declaration.
type Box[V any] struct{ val V }

func (b Box[E]) Get() E { return b.val }

func main() {
	s := &Stack[int]{}
	s.Push(1)
	s.Push(2)
	s.Push(3)
	fmt.Println("len:", s.Len())
	for s.Len() > 0 {
		v, _ := s.Pop()
		fmt.Println("pop:", v)
	}

	ss := &Stack[string]{}
	ss.Push("a")
	ss.Push("b")
	fmt.Println("slen:", ss.Len())

	b := Box[float64]{val: 3.5}
	fmt.Println("box:", b.Get())
}
