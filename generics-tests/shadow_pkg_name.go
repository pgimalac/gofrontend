// A local variable in a generic function/method body may shadow an imported
// package name (here "errors"), including when the package is also used
// elsewhere in the same body. The instantiation replay must keep the local
// variable, not rebind the name to the package.
package main

import (
	"errors"
	"fmt"
)

func classify[T any](v T, fail bool) error {
	if fail {
		return errors.New("boom")
	}
	var errors error // shadows the imported "errors" package
	_ = errors
	return nil
}

type Box[T any] struct{ v T }

func (b *Box[T]) Check(fail bool) error {
	if fail {
		return errors.New("method boom")
	}
	var errors error
	_ = errors
	return nil
}

func main() {
	fmt.Println(classify(1, false), classify(1, true))
	b := &Box[int]{v: 3}
	fmt.Println(b.Check(false), b.Check(true))
}
