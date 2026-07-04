// Regression: a generic call (slices.EqualFunc) whose inferred type argument
// is a cross-package type (resolver.Address) must resolve to the exact
// package even when another imported package shares the name "resolver".
// The instantiation re-parses the type argument tokens ("[]resolver.Address",
// "resolver.Address") and, before the fix, an ambiguous by-name lookup could
// bind "resolver" to the wrong same-named package, reporting Address as an
// undefined identifier.
package main

import (
	"fmt"
	"slices"

	ir "xtest/case_samename_pkg/base"
	resolver "xtest/case_samename_pkg/lib"
)

var _ = ir.Marker{}

func eq(a, b *resolver.Address) bool { return a.Addr == b.Addr }

func eqSlice(a, b []resolver.Address) bool {
	return slices.EqualFunc(a, b, func(a, b resolver.Address) bool { return eq(&a, &b) })
}

func main() {
	fmt.Println(eqSlice([]resolver.Address{{"x"}}, []resolver.Address{{"x"}}))
	fmt.Println(eqSlice([]resolver.Address{{"x"}}, []resolver.Address{{"y"}}))
}
