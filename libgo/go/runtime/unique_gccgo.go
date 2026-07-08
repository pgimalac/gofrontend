// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// gccgo implementation of the runtime hook used by the unique package to
// register a cleanup callback for its interning maps.
//
// In gc, the runtime periodically invokes this callback (after a GC) so that
// the unique package can prune map entries whose weak references have been
// collected.  gccgo's garbage collector does not support weak references
// (see weak_gccgo.go), so weakly-referenced values are never reclaimed and
// there is nothing to prune.  The registration is therefore a no-op: the
// callback is simply never called.

package runtime

import _ "unsafe" // for go:linkname

//go:linkname unique_runtime_registerUniqueMapCleanup runtime.unique_runtime_registerUniqueMapCleanup
func unique_runtime_registerUniqueMapCleanup(f func()) {
}
