package lib

import "xtest/case_xpkg_struct_typearg/base"

// Use an ANONYMOUS struct type with an unexported field as the type argument,
// so the instantiation serializes the struct's fields (not a type name).
func Use() int {
	v := struct {
		n int
		a any
	}{n: 7, a: "x"}
	r := base.Take(v) // inferred anonymous-struct type argument
	return r.n
}
