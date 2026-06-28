package main

import "fmt"

type Stringer interface {
	String() string
}

func JoinStrings[T Stringer](xs []T, sep string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += sep
		}
		out += x.String()
	}
	return out
}

type Color int

func (c Color) String() string {
	switch c {
	case 0:
		return "red"
	case 1:
		return "green"
	default:
		return "blue"
	}
}

func main() {
	fmt.Println(JoinStrings([]Color{0, 1, 2}, ", "))
}
