package main
import "fmt"
type Ordered interface{ Less(o interface{}) bool; Val() int }
type N int
func (n N) Less(o interface{}) bool { return int(n) < o.(N).Val() }
func (n N) Val() int { return int(n) }
func MaxOf[T Ordered](xs []T) T { m := xs[0]; for _, x := range xs { if m.Less(x) { m = x } }; return m }
func main() { fmt.Println(MaxOf([]N{3,7,2}).Val()) }
