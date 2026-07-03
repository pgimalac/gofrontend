// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !loong64

package goarch

// IsLoong64 is not generated into zgoarch.go by the gccgo build (loong64 is
// not in ALLGOARCH / configure.ac), so define it here. It is 0 on every
// architecture except loong64 (see goarch_loong64.go).
const IsLoong64 = 0
