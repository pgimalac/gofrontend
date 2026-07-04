// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"internal/coverage/rtcov"
	_ "unsafe" // for go:linkname
)

// runtime_coverage_getCovCounterList returns a list of counter-data
// blobs registered for the currently executing instrumented program.
//
// Upstream gc walks the moduledata covctrs/ecovctrs sections to collect
// the counter blobs. The gccgo frontend does not emit those moduledata
// sections (coverage instrumentation is not implemented in gccgo), so
// there are never any registered counter blobs; return an empty list.
// This keeps the runtime/coverage package linkable.
//
//go:linkname runtime_coverage_getCovCounterList runtime_1coverage.getCovCounterList
func runtime_coverage_getCovCounterList() []rtcov.CovCounterBlob {
	return []rtcov.CovCounterBlob{}
}
