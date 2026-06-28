package main

import "fmt"

type List[T any] struct {
	head *node[T]
}

type node[T any] struct {
	val  T
	next *node[T]
}

func (l *List[T]) Prepend(v T) {
	l.head = &node[T]{val: v, next: l.head}
}

func (l *List[T]) Slice() []T {
	var out []T
	for n := l.head; n != nil; n = n.next {
		out = append(out, n.val)
	}
	return out
}

func main() {
	l := &List[int]{}
	l.Prepend(1)
	l.Prepend(2)
	l.Prepend(3)
	fmt.Println(l.Slice())
}
