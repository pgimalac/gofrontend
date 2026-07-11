package main

import (
	"fmt"
	"iter"
)

func count(n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func main() {
	next, stop := iter.Pull(count(100))
	a, _ := next()
	b, _ := next()
	fmt.Println("pull:", a, b)
	stop()
	v, ok := next()
	fmt.Println("after stop:", v, ok)

	n2, s2 := iter.Pull2(func(yield func(int, string) bool) {
		yield(1, "a")
		yield(2, "b")
	})
	defer s2()
	fmt.Print("pull2:")
	for {
		k, val, ok := n2()
		if !ok {
			break
		}
		fmt.Printf(" %d%s", k, val)
	}
	fmt.Println()
}
