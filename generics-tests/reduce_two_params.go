package main
import "fmt"
func Reduce[T, U any](xs []T, init U, f func(U, T) U) U {
	acc := init
	for _, x := range xs { acc = f(acc, x) }
	return acc
}
func main() { fmt.Println(Reduce([]int{1,2,3,4}, 0, func(a, b int) int { return a+b })) }
