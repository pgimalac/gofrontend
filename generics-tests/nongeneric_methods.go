// Not a generics test: guards against regressions in ordinary method
// parsing, which shares the receiver-parsing path changed for generics.
package main

import "fmt"

type Counter struct{ n int }

func (c *Counter) Inc()     { c.n++ }
func (c Counter) Get() int  { return c.n }

type Named struct{ name string }

func (n Named) String() string { return "name=" + n.name }

func main() {
	c := &Counter{}
	c.Inc()
	c.Inc()
	c.Inc()
	fmt.Println(c.Get())
	fmt.Println(Named{name: "x"}.String())
}
