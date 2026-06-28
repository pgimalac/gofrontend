package main
import "fmt"
func Convert[T ~int](x int) T { return T(x) }
func main() { fmt.Println(Convert[int](42) + 1) }
