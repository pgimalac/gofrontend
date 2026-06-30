package main

import "fmt"

// Box has methods; inferring N from a Box[T] argument builds a marker
// signature whose parameter is Box[$infermarker] -- that transient instance
// must not drag in (and fail to instantiate) Box's methods.
type Box[N any] struct{ v N }

func (b Box[N]) Get() N { return b.v }

func Use[N any](b Box[N]) N { return b.Get() }

// A second generic that takes the interface a Box-derived value satisfies,
// inferring its type parameter from an argument whose type is itself a
// generic instance.
type Getter[N any] interface{ Get() N }

func ViaIface[N any](g Getter[N]) N { return g.Get() }

func main() {
	fmt.Println(Use(Box[int]{v: 7}))
	fmt.Println(ViaIface[int](Box[int]{v: 9}))
}
