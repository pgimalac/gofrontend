package main

import "fmt"

func seq(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func main() {
	// labeled break out of a range-over-func loop
	fmt.Print("break:")
outer1:
	for x := 0; x < 3; x++ {
		for v := range seq(3) {
			if v == 2 {
				break outer1
			}
			fmt.Printf(" %d/%d", x, v)
		}
	}
	fmt.Println()

	// labeled continue
	fmt.Print("continue:")
outer2:
	for x := 0; x < 3; x++ {
		for v := range seq(3) {
			if v == 1 {
				continue outer2
			}
			fmt.Printf(" %d/%d", x, v)
		}
	}
	fmt.Println()

	// goto out of the loop
	fmt.Print("goto:")
	for v := range seq(5) {
		if v == 3 {
			goto done
		}
		fmt.Printf(" %d", v)
	}
done:
	fmt.Println(" end")

	// nested range-over-func with labeled break across both levels
	fmt.Print("nested:")
outer3:
	for a := range seq(3) {
		for b := range seq(3) {
			if a == 1 && b == 1 {
				break outer3
			}
			fmt.Printf(" %d,%d", a, b)
		}
	}
	fmt.Println()
}
