// Regression: the importing package defines its OWN exported generic type
// with the same name ("Settings[T]") as one in the imported library.  When
// lib.NewQueue is instantiated here, its parameter type "Settings[T]" (a
// bare, unqualified reference in lib's source) must resolve to lib.Settings,
// NOT to this package's same-named Settings.  Before the fix,
// lookup_generic_type matched the local bare "Settings" first, so the
// re-parsed lib.NewQueue parameter became main.Settings[int], and passing a
// lib.Settings[int] composite literal to it failed with
// "cannot use type Settings$typeA as type Settings$typeB".
package main

import (
	"fmt"

	"xtest/case_samename_generic_type/lib"
)

// This package's own generic Settings, unrelated to lib.Settings.
type Settings[T any] struct {
	Name string
}

func describe[T any](s Settings[T]) string { return s.Name }

func main() {
	q := lib.NewQueue[int](lib.Settings[int]{Capacity: 42, NumConsumers: 3})
	local := Settings[int]{Name: "local"}
	fmt.Println(q.Cap(), describe(local))
}
