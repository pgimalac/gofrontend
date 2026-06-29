package main

import "xtest/case_constraint_bad/lib"

type Pt struct{ X int }

// A struct does not satisfy the cross-package named type-set constraint
// Ordered; this must be rejected.
func main() {
	_ = lib.Max(Pt{1}, Pt{2})
}
