package lib

type Box[T any] struct{ V T }

func Wrap[T any](v T) Box[T] { return Box[T]{V: v} }
func Unwrap[T any](b Box[T]) T { return b.V }
