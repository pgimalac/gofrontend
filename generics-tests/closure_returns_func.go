package main
import "fmt"
func MakeAdder[T int | float64](base T) func(T) T { return func(x T) T { return base + x } }
func main() { add5 := MakeAdder(5); fmt.Println(add5(3)) }
