package main

import (
	"fmt"

	"xtest/case_infer_xpkg_instance/lib"
)

func main() {
	h := lib.New[int64]()
	h.Grow(4)
	fmt.Println(h.Sum())

	g := lib.New[float64]()
	g.Grow(3)
	fmt.Println(g.Sum())
}
