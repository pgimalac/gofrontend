package main
import "fmt"
type Adder[T int|float64] struct{ base T }
func (a Adder[T]) Add(x T) T { return a.base + x }
func main() { a := Adder[int]{base: 10}; f := a.Add; fmt.Println(f(5)) }
