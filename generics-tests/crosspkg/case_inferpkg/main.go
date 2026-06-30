package main

import (
	"fmt"

	"xtest/case_inferpkg/base"
	"xtest/case_inferpkg/lib"
)

func main() {
	fmt.Println(lib.Pick(base.Unit(""), base.Unit("s")))
}
