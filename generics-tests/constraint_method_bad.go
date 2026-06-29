// Negative test: int does not implement the method-set constraint
// Stringer, even though the body never calls the method.
package main

type Stringer interface{ String() string }

func Use[T Stringer](v T) {}

func main() { Use(42) }
