// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build loong64

package goarch

const (
	_ArchFamily          = LOONG64
	_DefaultPhysPageSize = 16384
	_PCQuantum           = 4
	_MinFrameSize        = 8
	_StackAlign          = PtrSize
)

// IsLoong64 is not generated into zgoarch.go by the gccgo build (loong64 is
// not in ALLGOARCH / configure.ac), so define it here for loong64 builds.
const IsLoong64 = 1
