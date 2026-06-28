// Identifier-collision test: an identifier spelled like a type
// parameter used in a non-type position must NOT be substituted.
package main
import "fmt"
type S struct{ T int }
func Get[T any](s S) int { return s.T }
func main() { fmt.Println(Get[string](S{T: 9})) }