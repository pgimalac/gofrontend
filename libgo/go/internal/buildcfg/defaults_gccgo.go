// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Default values for the GOARM64 and GORISCV64 microarchitecture-level
// environment variables introduced in Go 1.23.
//
// For the gc toolchain these are generated into zbootstrap.go by the build.
// gccgo generates the other buildcfg defaults (defaultGOAMD64, defaultGOARM,
// ...) into buildcfg.go from Makefile.am; these two newer ones are provided
// here so the package builds.  The values match the gc defaults.

package buildcfg

const defaultGOARM64 = `v8.0`
const defaultGORISCV64 = `rva20u64`
