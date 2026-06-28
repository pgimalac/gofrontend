package main
import "fmt"
func Append[T any](s []T, xs ...T) []T { return append(s, xs...) }
func main() { fmt.Println(Append([]int{1}, 2, 3, 4)) }
