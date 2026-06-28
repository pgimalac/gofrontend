package main
import "fmt"
type Container[T any] interface { Get() T }
type Box[T any] struct{ v T }
func (b Box[T]) Get() T { return b.v }
func Show[T any](c Container[T]) T { return c.Get() }
func main() { fmt.Println(Show[int](Box[int]{v: 7})) }
