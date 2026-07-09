// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cgroup

// Functions below pushed from runtime.
// gccgo: the runtime pushes throw via a 2-arg //go:linkname
// (runtime.cgroup_throw -> internal/runtime/cgroup.throw), so here it is a
// plain bodyless declaration with no //go:linkname, matching how
// internal/sync declares the runtime-provided throw/fatal.

func throw(s string)
