// Regression: a generic call (EqualFunc) whose inferred type argument
// is a cross-package type (resolver.Address) must resolve to the exact
// package even when another imported package shares the name "resolver".
// The instantiation re-parses the type argument tokens ("[]resolver.Address",
// "resolver.Address") and, before the fix, an ambiguous by-name lookup could
// bind "resolver" to the wrong same-named package, reporting Address as an
// undefined identifier.
//
// Uses a LOCAL generic EqualFunc (not stdlib slices.EqualFunc) so the test
// exercises the 1.18 generics bug without depending on the Go 1.21 "slices"
// package -- the bug reproduces with any generic call inferring a
// cross-package type argument.
package main

import (
	"fmt"

	ir "xtest/case_samename_pkg/base"
	resolver "xtest/case_samename_pkg/lib"
)

var _ = ir.Marker{}

// EqualFunc reports whether a and b are element-wise equal under eq.
func EqualFunc[S ~[]E, E any](a, b S, eq func(E, E) bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !eq(a[i], b[i]) {
			return false
		}
	}
	return true
}

func eq(a, b *resolver.Address) bool { return a.Addr == b.Addr }

func eqSlice(a, b []resolver.Address) bool {
	return EqualFunc(a, b, func(a, b resolver.Address) bool { return eq(&a, &b) })
}

func main() {
	fmt.Println(eqSlice([]resolver.Address{{"x"}}, []resolver.Address{{"x"}}))
	fmt.Println(eqSlice([]resolver.Address{{"x"}}, []resolver.Address{{"y"}}))
}
