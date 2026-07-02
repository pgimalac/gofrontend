package main

import (
	"fmt"

	"xtest/case_genericxpkg/lib"
)

func main() {
	r := lib.Run[int64](func(b lib.Box[int64]) int64 { return b.V + 1 }, lib.Box[int64]{V: 5})
	fmt.Println(r)
}
