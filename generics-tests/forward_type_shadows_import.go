package main

import (
	"fmt"
	"go/types"
)

// Regression: a package-scope type used before its declaration (a forward
// reference) must not be hijacked to an imported package that merely shares
// its spelling.  Importing go/types transitively registers the package
// go/parser under the name "parser" (go/types.gox begins with
// "import parser go/parser ..."); the generics name-resolution last resort
// in Gogo::lookup, which resolves a bare name to a same-named known package,
// must fire only while re-parsing a generic template -- not for this
// ordinary forward reference -- or "var p parser" below wrongly resolves to
// the go/parser package and fails with "expected type".  This is the exact
// pattern in go/internal/gccgoimporter (imports go/types, has a local
// "type parser" used before its definition).

var _ = types.Universe // ensure go/types is imported and used

func makeParser() *parser {
	var p parser // forward reference to the local type below
	p.n = 40
	return &p
}

type parser struct {
	n int
}

func main() {
	p := makeParser()
	p.n += 2
	fmt.Println(p.n)
}
