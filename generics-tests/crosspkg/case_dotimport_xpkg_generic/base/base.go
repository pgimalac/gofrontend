package base

// The type argument, defined in a THIRD package (neither the caller nor the
// generic function's package).  Pointer receiver so *Item is the type used.
type Item struct{ Name string }

func (i *Item) String() string { return i.Name }
