// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import _ "unsafe" // for go:linkname

//go:linkname boring_runtime_arg0 crypto_1internal_1boring.runtime__arg0
func boring_runtime_arg0() string {
	// On Windows, argslice is not set, and it's too much work to find argv0.
	if len(argslice) == 0 {
		return ""
	}
	return argslice[0]
}

//go:linkname fipstls_runtime_arg0 crypto_1internal_1boring_1fipstls.runtime__arg0
func fipstls_runtime_arg0() string { return boring_runtime_arg0() }
