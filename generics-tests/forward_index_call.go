package main

import "fmt"

// Regression: "name[expr](args)" where "name" is a forward reference to a
// non-generic package-scope value (here an array of functions) used before
// its declaration.  The generics parser must not misread "[expr]" as a
// generic type-argument list for a call "F[T](args)"; it is an ordinary
// index of a value followed by a call.  This is the pattern used by
// html/template's transitionFunc (an "[...]func(...)" array indexed by a
// state before the array is declared, in an earlier-compiled file).

type state uint8

const (
	stateA state = iota
	stateB
)

type context struct {
	s state
}

// g uses transitionFunc, m, and s before their declarations below.
func g() int {
	total := 0
	c := context{s: stateB}
	total += transitionFunc[c.s](c)   // array-of-func index + call, forward
	total += m["x"]()                 // map index + call, forward
	total += s[0]()                   // slice index + call, forward
	return total
}

func tA(c context) int { return 1 }
func tB(c context) int { return 10 }

var transitionFunc = [...]func(context) int{
	stateA: tA,
	stateB: tB,
}

var m = map[string]func() int{
	"x": func() int { return 100 },
}

var s = []func() int{
	func() int { return 1000 },
}

func main() {
	fmt.Println(g())
}
