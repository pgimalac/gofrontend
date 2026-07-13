// Function-local types used as type arguments to a generic type and to
// generic functions (constrained and unconstrained). gccgo previously
// rejected the generic-type / constrained-function cases with
// "use of undefined type 'local'" because the local type was out of scope
// during generic instantiation re-parse.
package main

import (
	"fmt"
	"sort"
)

type Box[T any] struct{ v T }

func Id[T any](v T) T { return v }

func First[T any](s []T) T { return s[0] }

func Keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func main() {
	type local struct{ n int }
	x := Id[local](local{5})            // generic func, unconstrained
	b := Box[local]{v: local{7}}        // generic TYPE with local type arg
	f := First[local]([]local{{1}, {2}}) // generic func, local slice elem
	type key struct{ id int }
	m := map[key]local{{1}: {10}, {2}: {20}}
	ks := Keys[key, local](m)           // two local type args, comparable constraint
	sort.Slice(ks, func(i, j int) bool { return ks[i].id < ks[j].id })
	fmt.Println(x.n, b.v.n, f.n, len(ks), ks[0].id, ks[1].id)
}
