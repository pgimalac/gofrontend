package main
import "fmt"
func main() { var s Stack[int]; s.Push(5); fmt.Println(s.Top()) }
type Stack[T any] struct{ items []T }
func (s *Stack[T]) Push(x T) { s.items = append(s.items, x) }
func (s *Stack[T]) Top() T { return s.items[len(s.items)-1] }
