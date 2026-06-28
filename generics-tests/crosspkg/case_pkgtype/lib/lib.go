package lib

const Scale = 100

type Unit int

type Measure[T any] struct {
	Label T
	Value Unit
}

func (m Measure[T]) Scaled() int { return int(m.Value) * Scale }
