package main

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
)

func count(yield func(int) bool) {
	for i := 0; i < 3; i++ {
		if !yield(i) {
			return
		}
	}
}

func pairs(yield func(int, string) bool) {
	if !yield(1, "a") {
		return
	}
	if !yield(2, "b") {
		return
	}
}

func none(yield func() bool) {
	for i := 0; i < 3; i++ {
		if !yield() {
			return
		}
	}
}

func retFromFunc() int {
	for x := range count {
		if x == 2 {
			return x * 10
		}
	}
	return -1
}

func seq(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func main() {
	// 1. Basic single-value iteration.
	fmt.Print("basic:")
	for x := range count {
		fmt.Print(" ", x)
	}
	fmt.Println()

	// 2. Two-value iteration.
	fmt.Print("pairs:")
	for k, v := range pairs {
		fmt.Print(" ", k, v)
	}
	fmt.Println()

	// 3. break stops iteration.
	fmt.Print("break:")
	for x := range count {
		if x == 1 {
			break
		}
		fmt.Print(" ", x)
	}
	fmt.Println()

	// 4. continue skips an iteration.
	fmt.Print("continue:")
	for x := range count {
		if x == 1 {
			continue
		}
		fmt.Print(" ", x)
	}
	fmt.Println()

	// 5. return from the enclosing function.
	fmt.Println("return:", retFromFunc())

	// 6. Capture and mutate an outer variable.
	s := 0
	for x := range count {
		s += x
	}
	fmt.Println("sum:", s)

	// 7. Nested normal loop whose inner break does not escape.
	fmt.Print("nested:")
	for x := range count {
		for j := 0; j < 3; j++ {
			if j == 1 {
				break
			}
			fmt.Print(" ", x, j)
		}
	}
	fmt.Println()

	// 8a. iter.Seq iterator.
	sum := 0
	for x := range seq(4) {
		sum += x
	}
	fmt.Println("iter.Seq:", sum)

	// 8b. maps.Keys.
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	keys := []string{}
	for k := range maps.Keys(m) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println("maps.Keys:", keys)

	// 8c. slices.Values and slices.All (iter.Seq2).
	sl := []int{10, 20, 30}
	tot := 0
	for v := range slices.Values(sl) {
		tot += v
	}
	fmt.Println("slices.Values:", tot)
	for i, v := range slices.All(sl) {
		fmt.Println("slices.All:", i, v)
	}

	// 9. switch with break inside the body (targets the switch).
	fmt.Print("switch:")
	for x := range count {
		switch x {
		case 1:
			break
		default:
			fmt.Print(" ", x)
		}
	}
	fmt.Println()

	// 10. goto to a label inside the body.
	fmt.Print("goto:")
	for x := range count {
		if x == 1 {
			goto skip
		}
		fmt.Print(" ", x)
	skip:
	}
	fmt.Println()

	// 11. sink range variable and zero-argument yield.
	n := 0
	for range count {
		n++
	}
	c := 0
	for range none {
		c++
	}
	fmt.Println("counts:", n, c)
}
