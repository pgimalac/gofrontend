package main

import (
	"fmt"
	"xtest/case_pkgtype/lib"
)

func main() {
	m := lib.Measure[string]{Label: "temp", Value: 3}
	fmt.Println(m.Label, m.Scaled())
}
