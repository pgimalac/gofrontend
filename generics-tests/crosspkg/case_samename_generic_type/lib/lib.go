// Library package: defines an exported generic type Settings[T] and an
// exported generic function NewQueue whose parameter references Settings[T]
// by its bare (unqualified) name -- as it appears in this package's source.
package lib

type Settings[T any] struct {
	Capacity     int64
	NumConsumers int
}

type Queue[T any] struct {
	cap int64
}

func NewQueue[T any](set Settings[T]) Queue[T] {
	return Queue[T]{cap: set.Capacity}
}

func (q Queue[T]) Cap() int64 { return q.cap }
