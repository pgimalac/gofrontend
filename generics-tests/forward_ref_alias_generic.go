// A type alias to a generic instance written BEFORE the generic type is
// declared (a forward reference).  The instantiation is deferred to the
// post-parse pending-resolution pass; that pass must canonicalize the type
// arguments (e.g. "any" -> "interface{}") just like an inline use, so the
// alias and an inline use of the same instantiation ("MapV2[any]") share one
// instance object.  Otherwise the two objects each get the methods and a later
// method-receiver re-parse cross-wires them, yielding "redefinition of Set".
// Mirrors google.golang.org/grpc/resolver map.go ("type AddressMap =
// AddressMapV2[any]" declared above the type and its methods).
package main

import "fmt"

type Map = MapV2[any] // alias BEFORE MapV2 is declared (forward ref)

type entry[T any] struct{ v T }

type MapV2[T any] struct{ m map[string]entry[T] }

func (a *MapV2[T]) Set(k string, v T) { a.m[k] = entry[T]{v} }
func (a *MapV2[T]) Get(k string) (T, bool) {
	e, ok := a.m[k]
	return e.v, ok
}
func (a *MapV2[T]) Len() int { return len(a.m) }

func NewMap() *Map { return NewMapV2[any]() }
func NewMapV2[T any]() *MapV2[T] {
	return &MapV2[T]{m: make(map[string]entry[T])}
}

func main() {
	m := NewMap()
	m.Set("a", 1)
	v, ok := m.Get("a")
	fmt.Println(m.Len(), ok, v.(int))
}
