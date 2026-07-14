package main

import (
	"fmt"

	. "xtest/case_dotimport_xpkg_generic/base"
	"xtest/case_dotimport_xpkg_generic/lib"
)

func main() {
	xs := []*Item{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	// *Item is a pointer to a dot-imported type from package base; lib.Names is
	// a constrained generic func in package lib, which does not import base.
	// gccgo previously reported "use of undefined type 'Item'".
	fmt.Println(lib.Names[*Item](xs))
}
