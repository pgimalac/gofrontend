package base

// A generic function defined here; instantiating it from another package with
// a struct type argument that has unexported / interface fields must preserve
// the struct's origin-package field identity during the instantiation re-parse.
func Take[T any](v T) T { return v }
