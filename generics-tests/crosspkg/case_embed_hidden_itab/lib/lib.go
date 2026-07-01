package lib

import "xtest/case_embed_hidden_itab/base"

// Exporter embeds the base.Reader interface, exactly like
// go.opentelemetry.io/otel/exporters/prometheus.Exporter embeds
// metric.Reader.  Because base.Reader has an unexported method, embedding is
// the only way for a type outside package base to satisfy it, and the
// interface method table for (*Exporter, base.Reader) must be published by
// this package (which defines Exporter) so importers can reference it.
type Exporter struct {
	base.Reader
}

func New(n int) *Exporter {
	return base.Wrap(&Exporter{Reader: base.NewReader(n)})
}
