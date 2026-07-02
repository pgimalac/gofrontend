// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// gccgo: always use the pure-Go block (no assembly), so this build
// constraint is disabled ("//-go:build") to compile unconditionally.
//-go:build purego || (!amd64 && !386 && !ppc64le && !ppc64)

package notsha256

func block(dig *digest, p []byte) {
	blockGeneric(dig, p)
}
