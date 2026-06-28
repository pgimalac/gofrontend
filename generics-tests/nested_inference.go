package main
import "fmt"
func Map[T, U any](s []T, f func(T) U) []U { r := make([]U,0,len(s)); for _,v:=range s { r=append(r,f(v)) }; return r }
func main() {
	xs := []int{1,2,3}
	ys := Map(Map(xs, func(x int) int { return x+1 }), func(x int) string { return fmt.Sprint(x) })
	fmt.Println(ys)
}
