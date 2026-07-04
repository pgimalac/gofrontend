// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

<<<<<<< go/./runtime/stubs3.go
//-go:build !aix && !darwin && !freebsd && !openbsd && !plan9 && !solaris
=======
//go:build !aix && !darwin && !freebsd && !openbsd && !plan9 && !solaris && !wasip1
>>>>>>> /tmp/go121/src/./runtime/stubs3.go

package runtime

//go:wasmimport gojs runtime.nanotime1
func nanotime1() int64
