// A closure that captures an enclosing variable and references it several
// times as the index of a *forward-referenced* (declared-later) package
// name.  Parsing "Table[key]" while Table is still an unknown reference
// routes the index through a nested token-replay Parse; that re-parse must
// share the outer parse's enclosing-variable set, otherwise the captured
// variable is added to the closure struct type more than once and the
// closure construction ends up with "too few expressions for struct".
package main

import "fmt"

func run(f func()) { f() }

func main() {
	items := []string{"a", "b"}
	for _, it := range items {
		key := it + "!"
		run(func() {
			if Table[key] == nil {
				fmt.Println("nil", key)
				return
			}
			Table[key](key)
			fmt.Println(Names[key], key)
		})
	}
}

var Table = map[string]func(string){
	"a!": func(s string) { fmt.Println("fn", s) },
	"b!": func(s string) { fmt.Println("fn", s) },
}

var Names = map[string]string{"a!": "A", "b!": "B"}
