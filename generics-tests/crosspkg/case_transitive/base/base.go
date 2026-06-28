package base

type Box[T any] struct{ V T }

func Wrap[T any](v T) Box[T] { return Box[T]{V: v} }
func (b Box[T]) Get() T      { return b.V }
