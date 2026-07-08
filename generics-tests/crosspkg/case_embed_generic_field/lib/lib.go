// Library package: a non-generic struct that embeds a generic type instance
// by value.  The embedded field's name is the generic's base name
// ("SizeofFunc").  When this struct is exported, the embedded field is
// written anonymously; the importer must recover the field name from the
// (mangled) instance type name so a cross-package keyed composite literal
// "lib.BaseSizer{SizeofFunc: ...}" resolves the field.
package lib

type Request interface {
	ItemsCount() int
}

// An exported generic type embedded by value.
type SizeofFunc[T any] func(T) int64

func (f SizeofFunc[T]) Sizeof(t T) int64 { return f(t) }

type BaseSizer struct {
	SizeofFunc[Request]
}

// An unexported generic type embedded by value, exercised via an exported
// constructor so the importer can build the same struct with a keyed literal
// on the unexported (package-hidden) field name.
type box[T any] struct {
	val T
}

type Holder struct {
	box[int]
}

func (h Holder) Val() int { return h.box.val }

func NewHolder(v int) Holder {
	return Holder{box: box[int]{val: v}}
}
