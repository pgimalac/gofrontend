package main
import "fmt"
type Stringer interface{ String() string }
type Wrap[T any] struct{ v T }
func (w Wrap[T]) String() string { return fmt.Sprintf("%v", w.v) }
func main() { var s Stringer = Wrap[int]{v: 9}; fmt.Println(s.String()) }
