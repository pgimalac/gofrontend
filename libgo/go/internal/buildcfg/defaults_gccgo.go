// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Exported default values for the microarchitecture-level environment
// variables (GOAMD64, GOARM, GOARM64, ...).
//
// For the gc toolchain these DefaultGO* constants are generated into
// zbootstrap.go by the build.  gccgo instead generates the lowercase
// defaultGO* forms into buildcfg.go from Makefile.am (defaultGO386,
// defaultGOAMD64, defaultGOARM, defaultGOMIPS, defaultGOMIPS64,
// defaultGOPPC64).  Since Go 1.24 the buildcfg package refers to the
// exported DefaultGO* names, so we alias the generated lowercase values
// here and provide the newer defaults (GOARM64, GORISCV64, GOFIPS140)
// that gccgo's Makefile does not generate.  The values match the gc
// defaults.

package buildcfg

const DefaultGO386 = defaultGO386
const DefaultGOAMD64 = defaultGOAMD64
const DefaultGOARM = defaultGOARM
const DefaultGOARM64 = `v8.0`
const DefaultGOMIPS = defaultGOMIPS
const DefaultGOMIPS64 = defaultGOMIPS64
const DefaultGOPPC64 = defaultGOPPC64
const DefaultGORISCV64 = `rva20u64`
const DefaultGOFIPS140 = `off`
