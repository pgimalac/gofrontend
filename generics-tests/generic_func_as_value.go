package main
import "fmt"
func apply(f func(int) int, x int) int { return f(x) }
func Double[T int](x T) T { return x*2 }
func main() { fmt.Println(apply(Double[int], 21)) }
