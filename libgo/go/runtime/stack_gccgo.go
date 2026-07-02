// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// gccgo-specific stack support.

package runtime

// startingStackSize is the amount of stack that new goroutines start with.
//
// The gc runtime added adaptive starting stack sizes in Go 1.19 (see
// gcComputeStartingStackSize in gc's stack.go).  gccgo manages goroutine
// stacks through the C runtime with a fixed size, so there is nothing to
// compute here; startingStackSize is a constant reported by the
// /gc/stack/starting-size:bytes metric.
var startingStackSize uint32 = 8192

// gcComputeStartingStackSize is a no-op under gccgo, which does not use the
// gc runtime's adaptive stack-start machinery.
func gcComputeStartingStackSize() {}
