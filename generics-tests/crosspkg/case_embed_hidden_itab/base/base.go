package base

// Reader is an interface with an UNEXPORTED method.  Such a "hidden-method"
// interface can only be satisfied by a type in this package, or -- across
// packages -- by a type that embeds the interface (forwarding the
// unexported method).  This mirrors go.opentelemetry.io/otel/sdk/metric's
// Reader (with unexported methods like aggregation/register/temporality).
type Reader interface {
	Collect() int
	secret()
}

type manual struct{ n int }

func (m *manual) Collect() int { return m.n }
func (m *manual) secret()      {}

func NewReader(n int) Reader { return &manual{n: n} }

// A generic helper, to keep the case within the generics suite and mirror
// the generic option plumbing of the package that surfaced the bug.
func Wrap[T any](v T) T { return v }
