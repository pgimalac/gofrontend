package main

import (
	"fmt"
	"xtest/case_infer/lib"
)

func main() {
	b := lib.Wrap(42)
	fmt.Println(lib.Unwrap(b))
	fmt.Println(lib.Unwrap(lib.Wrap("hello")))
}
