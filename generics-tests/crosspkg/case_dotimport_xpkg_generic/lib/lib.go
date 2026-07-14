package lib

// A constrained generic function.  It does NOT import package base, so when it
// is instantiated with *base.Item the template re-parse happens in a scope that
// does not know that type -- the type argument must carry its origin package.
type Stringer interface{ String() string }

func Names[T Stringer](xs []T) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.String()
	}
	return out
}
