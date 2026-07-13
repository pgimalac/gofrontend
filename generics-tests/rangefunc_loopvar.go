package main

import (
	"fmt"
	"sort"
)

// A simple iter.Seq[int]-style function.
func count(n int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

// An iter.Seq2[int,string]-style function.
func pairs() func(yield func(int, string) bool) {
	data := []struct {
		k int
		v string
	}{{0, "a"}, {1, "b"}, {2, "c"}}
	return func(yield func(int, string) bool) {
		for _, p := range data {
			if !yield(p.k, p.v) {
				return
			}
		}
	}
}

func main() {
	// Case 1: single value, closures capturing v must see per-iteration values.
	var fns []func() int
	for v := range count(3) {
		fns = append(fns, func() int { return v })
	}
	var got1 []int
	for _, f := range fns {
		got1 = append(got1, f())
	}
	fmt.Println("case1", got1) // want [0 1 2]

	// Case 2: address-of, each &v distinct.
	var ptrs []*int
	for v := range count(3) {
		v := v
		_ = v
		ptrs = append(ptrs, &v)
	}
	// re-run without inner redeclare: loopvar itself must be per-iteration
	var ptrs2 []*int
	for v := range count(3) {
		ptrs2 = append(ptrs2, &v)
	}
	var deref []int
	for _, p := range ptrs2 {
		deref = append(deref, *p)
	}
	fmt.Println("case2", deref) // want [0 1 2]
	_ = ptrs

	// Case 3: two-value range-func, capture both k and v.
	var kvs []func() string
	for k, v := range pairs() {
		kvs = append(kvs, func() string { return fmt.Sprintf("%d:%s", k, v) })
	}
	var got3 []string
	for _, f := range kvs {
		got3 = append(got3, f())
	}
	sort.Strings(got3)
	fmt.Println("case3", got3) // want [0:a 1:b 2:c]

	// Case 4: nested range-func, inner closures capture both loop vars.
	var nested []func() string
	for a := range count(2) {
		for b := range count(2) {
			nested = append(nested, func() string { return fmt.Sprintf("%d%d", a, b) })
		}
	}
	var got4 []string
	for _, f := range nested {
		got4 = append(got4, f())
	}
	sort.Strings(got4)
	fmt.Println("case4", got4) // want [00 01 10 11]

	// Case 5: mutation within iteration is visible; next iteration is fresh.
	sum := 0
	for v := range count(4) {
		v += 10
		sum += v
	}
	fmt.Println("case5 sum", sum) // want 10+11+12+13 = 46
}
