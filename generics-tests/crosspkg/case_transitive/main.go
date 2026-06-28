package main

import (
	"fmt"
	"xtest/case_transitive/base"
	"xtest/case_transitive/lib"
)

func main() {
	fmt.Println(lib.WrapOnce(7).Get())
	fmt.Println(lib.Unwrap(base.Wrap("deep")))
}
