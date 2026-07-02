// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sig holds “code signatures” that can be called
// and will result in certain code sequences being linked into
// the final binary. The functions themselves are no-ops.
package sig

// gccgo: gc implements these no-op marker functions in per-architecture
// assembly (sig_amd64.s etc.).  gccgo has no assembly for them, so provide
// empty Go bodies -- the functions exist only so that references to them pull
// specific code sequences into the final binary; the bodies do nothing.

// BoringCrypto indicates that the BoringCrypto module is present.
func BoringCrypto() {}

// FIPSOnly indicates that package crypto/tls/fipsonly is present.
func FIPSOnly() {}

// StandardCrypto indicates that standard Go crypto is present.
func StandardCrypto() {}
