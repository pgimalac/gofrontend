// Regression: instantiating (via argument inference) a cross-package generic
// function whose type-parameter constraint is a named interface of the
// defining package.  Resolving the constraint during the deferred check ran
// outside the defining package's context and parsed the constraint's bare
// names quietly, but then called interface_type() on the resulting unresolved
// forward declaration, which forced a spurious "use of undefined type
// Contents/Marshallable" error.
package main

import (
	"fmt"

	"xtest/case_named_constraint_iface/lib"
)

func main() {
	a := lib.Wrap(1, &lib.A{})
	fmt.Println(a != nil)
	fmt.Println(lib.Show(2, &lib.A{}))
	fmt.Println(lib.Show(3, &lib.B{}))
}
