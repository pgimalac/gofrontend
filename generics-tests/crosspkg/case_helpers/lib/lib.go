package lib

func Double(x int) int { return x * 2 }
func triple(x int) int { return x * 3 } // unexported

func Apply[T any](v T, f func(T) T) T { return f(v) }

func DoubleLen[T any](xs []T) int { return Double(len(xs)) }
func TripleLen[T any](xs []T) int { return triple(len(xs)) }
func ApplyTwice[T any](v T, f func(T) T) T { return Apply(Apply(v, f), f) }
