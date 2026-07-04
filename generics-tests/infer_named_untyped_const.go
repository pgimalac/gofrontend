// Type inference must not be broken by a named untyped constant argument.
// A named untyped constant (unlike a bare literal) shares its underlying
// Named_constant; determining its type during inference must not poison
// the real argument so that it can no longer take the parameter's type.
package main

import "math"

func setDefault[T ~int | ~int32 | ~uint32 | ~int64](v *T, minval, maxval, defval T) {
	if *v == 0 {
		*v = defval
	}
	_ = minval
	_ = maxval
}

const small = 100

func id[T ~int | ~int64](x T) T { return x }

func main() {
	var a uint32
	setDefault(&a, 1, small, 42) // named const 'small' as a T-typed arg
	println(a)

	var b uint32
	setDefault(&b, 1, math.MaxUint32, 7) // imported named const
	println(b)

	println(id(small)) // untyped const as the sole type determiner
	println(id(5))     // bare literal still works
}
