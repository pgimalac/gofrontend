package main

import (
	"fmt"

	"xtest/case_genericxpkg_import/lib"
)

// main imports lib but NOT base. Instantiating lib.Box[int64] (whose Items
// field's element type is base.KV) must resolve base.KV even though base is
// only genimported by the compiler.
func main() {
	var b lib.Box[int64]
	b.N = 41
	fmt.Println(b.N + int64(len(b.Items)))
}
