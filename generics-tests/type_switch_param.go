package main
import "fmt"
func Describe[T any](x T) string { var i interface{} = x; switch i.(type) { case int: return "int"; case string: return "string"; default: return "other" } }
func main() { fmt.Println(Describe(1), Describe("a"), Describe(1.5)) }
