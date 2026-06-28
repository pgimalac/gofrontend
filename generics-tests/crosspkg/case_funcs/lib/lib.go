package lib

type Ordered interface{ ~int | ~int64 | ~float64 | ~string }

func Max[T Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func Map[T, U any](s []T, f func(T) U) []U {
	r := make([]U, len(s))
	for i, v := range s {
		r[i] = f(v)
	}
	return r
}
