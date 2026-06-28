package main
import "fmt"
type MyInt int
func Sum[T ~int | ~float64](xs []T) T { var t T; for _, x := range xs { t += x }; return t }
func main() { fmt.Println(Sum([]MyInt{1,2,3})) }
