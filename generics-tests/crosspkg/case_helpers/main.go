package main

import (
	"fmt"
	"xtest/case_helpers/lib"
)

func main() {
	fmt.Println(lib.DoubleLen([]int{1, 2, 3}))
	fmt.Println(lib.TripleLen([]string{"a", "b"}))
	fmt.Println(lib.ApplyTwice(2, func(x int) int { return x + 10 }))
}
