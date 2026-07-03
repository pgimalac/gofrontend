package base

// KV is a type in a package that the top-level program does NOT import
// directly; it is only reached via lib's generic type's field.  When lib is
// imported, base.KV is inlined into lib's export data as a not-yet-visible
// type; the compiler must still accept it when instantiating lib's generic in
// the importer.
type KV struct {
	Key   string
	Value int
}
