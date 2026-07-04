// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build riscv64

package cpu

<<<<<<< go/./golang.org/x/sys/cpu/cpu_riscv64.go
// const cacheLineSize = 32
=======
const cacheLineSize = 64
>>>>>>> /tmp/go122/src/./vendor/golang.org/x/sys/cpu/cpu_riscv64.go

func initOptions() {}
