// Negative test: string does not satisfy int | float64.
package main
import "fmt"
func Min[T int | float64](a, b T) T { if a < b { return a }; return b }
func main() { fmt.Println(Min[string]("a", "b")) }
