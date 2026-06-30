package main

import "fmt"

type Aggregation interface{ aggregate() }
type Default struct{}

func (Default) aggregate() {}

// A generic method body is captured as tokens and only parsed when
// instantiated; a "case nil" inside it must still lower to a nil comparison
// (not a type descriptor of the nil type).
type inserter[N int64 | float64] struct{}

func (i *inserter[N]) classify(a Aggregation) string {
	switch a.(type) {
	case nil, Default:
		return "default"
	default:
		return "other"
	}
}

func main() {
	var i inserter[int64]
	fmt.Println(i.classify(nil))
	fmt.Println(i.classify(Default{}))
	var f inserter[float64]
	fmt.Println(f.classify(nil))
}
