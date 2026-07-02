package main

import "fmt"

const N = 512

// Regression: an array type whose length is a BINARY-OP expression on a
// named constant, together with a method on that type, was misparsed as a
// generic type parameter list ("[N ...]"), leaving the type undefined.
type Bits [N / 64]uint64

func (b *Bits) set(i uint)      { b[i/64] |= 1 << (i % 64) }
func (b *Bits) get(i uint) bool { return b[i/64]&(1<<(i%64)) != 0 }

// A 2-D array bound and other operators must also work with methods.
type Grid [N / 128][N - 508]int

func (g *Grid) at(r, c int) int { return g[r][c] }

// Real generic types must still parse correctly (not broken by the fix).
type Box[T any] struct{ v T }

func (b Box[T]) Get() T { return b.v }

func main() {
	var b Bits
	b.set(3)
	b.set(130)
	fmt.Println(len(b), b.get(3), b.get(4), b.get(130))

	var g Grid
	g[1][2] = 7
	fmt.Println(len(g), len(g[0]), g.at(1, 2))

	fmt.Println(Box[int]{v: 42}.Get(), Box[string]{v: "hi"}.Get())
}
