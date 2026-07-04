package main

import "fmt"

const cmin = min(3, 5)
const cmax = max(3, 5)
const cf = min(3.5, 2.25, 9.0)

func main() {
	// min/max with ints, 2 and 3+ args
	fmt.Println(min(3, 5), max(3, 5))
	fmt.Println(min(7, 2, 9, 1, 4), max(7, 2, 9, 1, 4))

	// floats
	fmt.Println(min(3.5, 2.25, 9.0), max(3.5, 2.25, 9.0))

	// strings
	fmt.Println(min("banana", "apple", "cherry"), max("banana", "apple", "cherry"))

	// constant results
	fmt.Println(cmin, cmax, cf)

	// non-constant args
	x, y, z := 10, 3, 7
	fmt.Println(min(x, y, z), max(x, y, z))

	// single arg
	fmt.Println(min(42), max(42))

	// clear on a map
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	clear(m)
	fmt.Println("map:", len(m))

	// clear on a slice of ints (no pointers)
	s := []int{1, 2, 3, 4}
	clear(s)
	fmt.Println("slice:", s, len(s))

	// clear on a slice with pointers
	ps := []*int{&x, &y, &z}
	clear(ps)
	fmt.Println("ptrslice:", ps[0] == nil, ps[1] == nil, ps[2] == nil)

	// clear on a slice of strings (has pointers)
	ss := []string{"x", "y", "z"}
	clear(ss)
	fmt.Printf("strslice: %q\n", ss)
}
