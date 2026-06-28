package main

import (
	"fmt"
	"sort"
)

type Number interface{ ~int | ~int64 | ~float64 }

func Map[T, U any](s []T, f func(T) U) []U {
	r := make([]U, len(s))
	for i, v := range s {
		r[i] = f(v)
	}
	return r
}

func Filter[T any](s []T, pred func(T) bool) []T {
	var r []T
	for _, v := range s {
		if pred(v) {
			r = append(r, v)
		}
	}
	return r
}

func Reduce[T, U any](s []T, init U, f func(U, T) U) U {
	acc := init
	for _, v := range s {
		acc = f(acc, v)
	}
	return acc
}

func Sum[T Number](s []T) T {
	return Reduce(s, T(0), func(a, b T) T { return a + b })
}

func Keys[K comparable, V any](m map[K]V) []K {
	r := make([]K, 0, len(m))
	for k := range m {
		r = append(r, k)
	}
	return r
}

func Max[T Number](s []T) T {
	m := s[0]
	for _, v := range s[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

type Stack[T any] struct{ data []T }

func (s *Stack[T]) Push(x T) { s.data = append(s.data, x) }
func (s *Stack[T]) Pop() T {
	x := s.data[len(s.data)-1]
	s.data = s.data[:len(s.data)-1]
	return x
}
func (s *Stack[T]) Empty() bool { return len(s.data) == 0 }

func main() {
	nums := []int{5, 2, 8, 1, 9, 3}
	doubled := Map(nums, func(x int) int { return x * 2 })
	fmt.Println("doubled:", doubled)
	evens := Filter(nums, func(x int) bool { return x%2 == 0 })
	fmt.Println("evens:", evens)
	fmt.Println("sum:", Sum(nums))
	fmt.Println("max:", Max(nums))
	fmt.Println("sumf:", Sum([]float64{1.5, 2.5, 3.0}))

	strs := Map(nums, func(x int) string { return fmt.Sprintf("#%d", x) })
	fmt.Println("strs:", strs)

	m := map[string]int{"a": 1, "b": 2, "c": 3}
	ks := Keys(m)
	sort.Strings(ks)
	fmt.Println("keys:", ks)

	st := &Stack[string]{}
	st.Push("x"); st.Push("y"); st.Push("z")
	for !st.Empty() {
		fmt.Print(st.Pop(), " ")
	}
	fmt.Println()
}
