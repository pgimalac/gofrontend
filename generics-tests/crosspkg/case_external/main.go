package main

import (
	"fmt"
	"xtest/case_external/lib"
)

func main() {
	fmt.Println(lib.Describe(42))
	fmt.Println(lib.Describe[string]("hi"))
	fmt.Println(lib.Join([]int{1, 2, 3}))
}
