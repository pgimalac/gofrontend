// Negative test: a named type whose underlying is int does not satisfy
// the *exact* (non-~) type set int | float64.
package main
import "fmt"
type MyInt int
func Min[T int | float64](a, b T) T { if a < b { return a }; return b }
func main() { var a, b MyInt = 3, 5; fmt.Println(Min(a, b)) }
