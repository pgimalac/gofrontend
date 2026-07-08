// Regression: constructing (via a keyed composite literal) an imported struct
// that embeds a generic type instance.  The embedded field name is the
// generic's base name, which is lost in export data (the field is anonymous)
// and must be recovered from the instance type name on import.
package main

import (
	"fmt"

	"xtest/case_embed_generic_field/lib"
)

type req struct{ n int }

func (r req) ItemsCount() int { return r.n }

func main() {
	bs := lib.BaseSizer{
		SizeofFunc: func(r lib.Request) int64 { return int64(r.ItemsCount()) },
	}
	fmt.Println(bs.Sizeof(req{n: 5}))

	h := lib.NewHolder(9)
	fmt.Println(h.Val())
}
