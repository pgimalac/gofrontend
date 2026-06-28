package main
import "fmt"
type Iter[T any] interface{ Next() (T, bool) }
type sliceIter[T any] struct{ s []T; i int }
func (it *sliceIter[T]) Next() (T, bool) { var z T; if it.i >= len(it.s) { return z, false }; v := it.s[it.i]; it.i++; return v, true }
func Collect[T any](it Iter[T]) []T { var out []T; for { v, ok := it.Next(); if !ok { break }; out = append(out, v) }; return out }
func main() { fmt.Println(Collect[int](&sliceIter[int]{s: []int{1,2,3}})) }
