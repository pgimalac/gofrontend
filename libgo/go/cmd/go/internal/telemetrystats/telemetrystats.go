// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package telemetrystats is a gccgo no-op stub: gccgo's go tool does not
// collect telemetry, so it records no build/version statistics.
package telemetrystats

func Increment() {}
