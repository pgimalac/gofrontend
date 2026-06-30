package main

import (
	"fmt"

	"xtest/case_embed/lib"
)

func main() {
	a := lib.New[int64]()
	a.Aggregate(5)
	a.Aggregate(7)
	fmt.Println(a.Sum())
}
