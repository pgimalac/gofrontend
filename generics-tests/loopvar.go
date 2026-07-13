package main

import (
	"fmt"
	"sort"
)

func main() {
	// 1. range-over-slice closure capture
	var a []func() int
	for _, v := range []int{1, 2, 3} {
		a = append(a, func() int { return v })
	}
	for _, f := range a {
		fmt.Print(f(), " ")
	}
	fmt.Println()

	// 2. 3-clause for closure capture
	var b []func() int
	for i := 0; i < 3; i++ {
		b = append(b, func() int { return i })
	}
	for _, f := range b {
		fmt.Print(f(), " ")
	}
	fmt.Println()

	// 3. multiple loop vars (index + value)
	var c []func() (int, int)
	for i, v := range []int{10, 20, 30} {
		c = append(c, func() (int, int) { return i, v })
	}
	for _, f := range c {
		i, v := f()
		fmt.Printf("%d:%d ", i, v)
	}
	fmt.Println()

	// 4. address-of loop var
	var p []*int
	for i := 0; i < 3; i++ {
		p = append(p, &i)
	}
	for _, ptr := range p {
		fmt.Print(*ptr, " ")
	}
	fmt.Println()

	// 5. nested loops
	var n []func() (int, int)
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			n = append(n, func() (int, int) { return i, j })
		}
	}
	for _, f := range n {
		i, j := f()
		fmt.Printf("%d%d ", i, j)
	}
	fmt.Println()

	// 6. loop var mutated in body; post sees it (continuity)
	sum := 0
	for i := 0; i < 5; i++ {
		if i == 2 {
			i++ // body mutation must carry to post
		}
		sum += i
	}
	fmt.Println("sum", sum)

	// 7. range-over-map capture (sorted for determinism)
	var m []func() int
	for _, v := range map[string]int{"a": 1, "b": 2, "c": 3} {
		m = append(m, func() int { return v })
	}
	var got []int
	for _, f := range m {
		got = append(got, f())
	}
	sort.Ints(got)
	fmt.Println(got)

	// 8. pre-declared var (NOT :=): stays shared per Go semantics
	var d []func() int
	k := 0
	for k = 0; k < 3; k++ {
		d = append(d, func() int { return k })
	}
	for _, f := range d {
		fmt.Print(f(), " ")
	}
	fmt.Println("(shared: 3 3 3)")
}
