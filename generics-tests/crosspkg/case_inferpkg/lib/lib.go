package lib

func nz[T comparable](v, alt T) T {
	var z T
	if v != z {
		return v
	}
	return alt
}

// Pick is instantiated with a type argument from a third package (base.Unit);
// its body's call to nz triggers a nested instantiation whose inferred type
// argument must be emitted and re-parsed qualified ("base.Unit").
func Pick[T comparable](a, b T) T { return nz(a, b) }
