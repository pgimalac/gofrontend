package main
import "fmt"
func Max[T int | float64](xs ...T) T { m := xs[0]; for _, x := range xs { if x > m { m = x } }; return m }
func main() { fmt.Println(Max(3,7,2,9,1)) }
