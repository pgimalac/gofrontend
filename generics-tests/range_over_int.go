package main

import "fmt"

// Go 1.22 "range over integer": for i := range n / for range n where n has
// integer type iterates i = 0,1,...,n-1.

type C int

func main() {
	// index only, integer literal
	for i := range 5 {
		fmt.Print(i)
	}
	fmt.Println()

	// no iteration variable, counter
	count := 0
	for range 3 {
		count++
	}
	fmt.Println(count)

	// variable bound
	n := 4
	for i := range n {
		fmt.Print(i)
	}
	fmt.Println()

	// named integer type: i has type C
	var c C = 3
	for i := range c {
		var x C = i // must typecheck: i is C
		fmt.Print(int(x))
	}
	fmt.Println()

	// zero and negative bounds: zero iterations
	z := 0
	for range z {
		fmt.Println("BAD zero")
	}
	neg := -2
	for i := range neg {
		fmt.Println("BAD neg", i)
	}
	fmt.Println("no-iter ok")

	// nested
	for i := range 3 {
		for j := range 2 {
			fmt.Printf("%d ", i*10+j)
		}
	}
	fmt.Println()

	// sum using range over integer
	sum := 0
	for i := range 10 {
		sum += i
	}
	fmt.Println(sum)
}
