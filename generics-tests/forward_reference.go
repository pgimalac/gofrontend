package main

import "fmt"

func main() {
	fmt.Println(Twice(21))
	fmt.Println(Twice("ab"))
}

func Twice[T any](x T) [2]T { return [2]T{x, x} }
