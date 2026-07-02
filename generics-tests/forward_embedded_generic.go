package main

import "fmt"

// Regression: a generic type used as an embedded struct field (both value and
// pointer forms) *before* the generic type is declared (forward reference).
// The embedded field must be named for the generic ("box"), so the field is
// reachable as ".box".  Exercises make_pending_generic_type plus the resolved
// alias's generic_base_name (consumed by Struct_field::field_name).
// (Promoted methods from a forward-referenced embedded generic are a separate,
// still-unsupported case and are intentionally not exercised here.)

type wrapV struct{ box[int] }     // value embed, forward reference
type wrapP struct{ *box[string] } // pointer embed, forward reference

type box[T any] struct{ v T }

func main() {
	wv := wrapV{box[int]{v: 7}}
	wp := wrapP{&box[string]{v: "hi"}}
	fmt.Println(wv.box.v, wp.box.v)
}
