// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package telemetry is a gccgo stub for the go command's telemetry hooks.
// gccgo's go tool does not collect or upload telemetry, so every entry point
// is a no-op (mirroring gc's cmd_go_bootstrap build of this package).
package telemetry

func MaybeParent()              {}
func MaybeChild()               {}
func Mode() string              { return "" }
func SetMode(mode string) error { return nil }
func Dir() string               { return "" }
func Start()                    {}
func StartWithUpload()          {}
