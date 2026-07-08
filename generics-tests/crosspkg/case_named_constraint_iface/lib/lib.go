// Library package with generic functions whose constraints are named
// interface types of this package -- one a pure type-set union, one that
// embeds a named interface (a method set) plus a type-set union, modelled on
// go-tpm's TPMU union constructors (NewTPMUPublicID[C PublicIDContents]).
//
// When such a generic is instantiated in another package (via inference from
// the argument), resolving the constraint must not report the constraint's
// bare type names (Contents/Marshallable) -- which are this package's types --
// as "use of undefined type".
package lib

type Marshallable interface {
	marshal() string
}

type A struct{ v string }

func (a *A) marshal() string { return "A:" + a.v }

type B struct{ v string }

func (b *B) marshal() string { return "B:" + b.v }

// A named pure type-set constraint.
type Contents interface {
	*A | *B
}

// A named constraint embedding a method-set interface plus a type-set union.
type MarshalContents interface {
	Marshallable
	*A | *B
}

func Wrap[C Contents](sel int, c C) C { return c }

func Show[C MarshalContents](sel int, c C) string { return c.marshal() }
