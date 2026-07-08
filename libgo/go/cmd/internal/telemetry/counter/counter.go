// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package counter is a gccgo stub for the go command's telemetry counters.
// gccgo's go tool does not collect telemetry, so all counters are no-ops
// (mirroring gc's cmd_go_bootstrap build of this package).
package counter

import "flag"

type dummyCounter struct{}

func (dc dummyCounter) Inc() {}

func Open()                                                               {}
func Inc(name string)                                                     {}
func New(name string) dummyCounter                                        { return dummyCounter{} }
func NewStack(name string, depth int) dummyCounter                        { return dummyCounter{} }
func CountFlags(name string, flagSet flag.FlagSet)                        {}
func CountFlagValue(prefix string, flagSet flag.FlagSet, flagName string) {}
