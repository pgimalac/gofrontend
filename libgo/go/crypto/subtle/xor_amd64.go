// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This declares an assembly xorBytes that gccgo does not provide;
// the portable implementation in xor_generic.go is used instead.
//go:build ignore

package subtle

//go:noescape
func xorBytes(dst, a, b *byte, n int)
