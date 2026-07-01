package main

import (
	"fmt"

	"xtest/case_embed_hidden_itab/base"
	"xtest/case_embed_hidden_itab/lib"
)

func use(r base.Reader) int { return r.Collect() }

func main() {
	// Converts *lib.Exporter to the hidden-method interface base.Reader,
	// which needs the interface method table published by package lib.
	fmt.Println(use(lib.New(7)))
}
