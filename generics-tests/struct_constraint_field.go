// A type parameter constrained to an anonymous struct, inferred from
// other parameters, with unexported fields accessed by the caller.
package main

import "fmt"

func make5[A interface {
	struct {
		b B
		c C
	}
}, B any, C interface{ *B }](x B) A {
	var a A
	return a
}

func main() {
	x := make5(1.5)
	var pb float64 = x.b
	var pc *float64 = x.c
	fmt.Println(pb, pc == nil)
}
