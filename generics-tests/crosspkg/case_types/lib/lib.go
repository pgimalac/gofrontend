package lib

type Stack[T any] struct {
	items []T
}

func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v, true
}
func (s *Stack[T]) Len() int { return len(s.items) }

type Pair[A, B any] struct {
	First  A
	Second B
}

func MakePair[A, B any](a A, b B) Pair[A, B] { return Pair[A, B]{First: a, Second: b} }
