package main
import "fmt"
type Pair[A, B any] struct{ First A; Second B }
func (p Pair[A, B]) Swap() Pair[B, A] { return Pair[B, A]{First: p.Second, Second: p.First} }
func main() { p := Pair[int, string]{First: 1, Second: "x"}; q := p.Swap(); fmt.Println(q.First, q.Second) }
