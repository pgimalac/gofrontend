// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build aix || darwin || dragonfly || freebsd || hurd || (js && wasm) || linux || netbsd || openbsd || solaris || windows || plan9

package runtime

// setenv_c and unsetenv_c update the C environment. They are
// implemented in the C runtime (runtime/go-setenv.c and
// runtime/go-unsetenv.c), which exports them under the syscall
// package's mangled names. They are called from syscall_runtimeSetenv
// and syscall_runtimeUnsetenv in runtime.go.
//
//go:linkname setenv_c syscall.setenv_c
func setenv_c(k string, v string)

//go:linkname unsetenv_c syscall.unsetenv_c
func unsetenv_c(k string)

func gogetenv(key string) string {
	env := environ()
	if env == nil {
		throw("getenv before env init")
	}
	for _, s := range env {
		if len(s) > len(key) && s[len(key)] == '=' && envKeyEqual(s[:len(key)], key) {
			return s[len(key)+1:]
		}
	}
	return ""
}

// envKeyEqual reports whether a == b, with ASCII-only case insensitivity
// on Windows. The two strings must have the same length.
func envKeyEqual(a, b string) bool {
	if GOOS == "windows" { // case insensitive
		for i := 0; i < len(a); i++ {
			ca, cb := a[i], b[i]
			if ca == cb || lowerASCII(ca) == lowerASCII(cb) {
				continue
			}
			return false
		}
		return true
	}
	return a == b
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
