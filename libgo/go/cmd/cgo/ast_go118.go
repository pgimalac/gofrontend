// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !compiler_bootstrap

package main

// As of Go 1.26 the handling of the generic AST nodes that previously lived in
// this file (File.walkUnexpected, covering *ast.IndexListExpr and friends) was
// folded directly into File.walk in ast.go, and upstream gc deleted this file.
//
// The gccgo gotools build (shared GCC-tree gotools/Makefile) still lists
// cmd/cgo/ast_go118.go in the cgo source set for every Go version, and that
// Makefile cannot be edited without breaking the pre-1.26 branches that do keep
// their walkUnexpected here. This intentionally empty translation unit keeps the
// file present so the cgo tool links, while the actual logic lives in ast.go.
