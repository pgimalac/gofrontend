// Identifier-collision test: an identifier spelled like a type
// parameter used in a non-type position must NOT be substituted.
package main
import "fmt"
type Box[T any] struct{ T T }
func main() { b := Box[int]{T: 5}; fmt.Println(b.T) }