package main

import (
	"fmt"

	"xtest/case_embedptr/lib"
)

func main() {
	a := lib.New[int64]()
	a.Aggregate(5)
	a.Aggregate(7)
	fmt.Println(a.Sum())
	b := lib.New[float64]()
	b.Aggregate(1)
	b.Aggregate(2)
	fmt.Println(b.Sum())
}
