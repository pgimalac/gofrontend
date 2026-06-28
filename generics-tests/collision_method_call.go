// Identifier-collision test: an identifier spelled like a type
// parameter used in a non-type position must NOT be substituted.
package main
import "fmt"
type C struct{}
func (C) T() string { return "hi" }
func Call[T any](c C) string { return c.T() }
func main() { fmt.Println(Call[int](C{})) }