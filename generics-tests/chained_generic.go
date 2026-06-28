package main
import "fmt"
func Wrap[T any](x T) []T { return []T{x} }
func main() { fmt.Println(Wrap(Wrap(7))) }
