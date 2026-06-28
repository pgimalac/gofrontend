package main
import "fmt"
func Index[T comparable](s []T, x T) int { for i, v := range s { if v == x { return i } }; return -1 }
func main() { fmt.Println(Index([]string{"a","b","c"}, "b")) }
