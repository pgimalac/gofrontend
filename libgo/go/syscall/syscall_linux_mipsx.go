// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (mips || mipsle || mips64 || mips64le || mips64p32 || mips64p32le)

package syscall

import "unsafe"

func (r *PtraceRegs) PC() uint64 {
	return r.Cp0_epc
}

func (r *PtraceRegs) SetPC(pc uint64) {
	r.Cp0_epc = pc
}

func PtraceGetRegs(pid int, regsout *PtraceRegs) (err error) {
	return ptrace(PTRACE_GETREGS, pid, 0, uintptr(unsafe.Pointer(regsout)))
}

<<<<<<< go/./syscall/syscall_linux_mipsx.go
func PtraceSetRegs(pid int, regs *PtraceRegs) (err error) {
	return ptrace(PTRACE_SETREGS, pid, 0, uintptr(unsafe.Pointer(regs)))
=======
func setTimespec(sec, nsec int64) Timespec {
	return Timespec{Sec: int32(sec), Nsec: int32(nsec)}
}

func setTimeval(sec, usec int64) Timeval {
	return Timeval{Sec: int32(sec), Usec: int32(usec)}
}

//sys	mmap2(addr uintptr, length uintptr, prot int, flags int, fd int, pageOffset uintptr) (xaddr uintptr, err error)

func mmap(addr uintptr, length uintptr, prot int, flags int, fd int, offset int64) (xaddr uintptr, err error) {
	page := uintptr(offset / 4096)
	if offset != int64(page)*4096 {
		return 0, EINVAL
	}
	return mmap2(addr, length, prot, flags, fd, page)
}

const rlimInf32 = ^uint32(0)
const rlimInf64 = ^uint64(0)

type rlimit32 struct {
	Cur uint32
	Max uint32
}

//sysnb getrlimit(resource int, rlim *rlimit32) (err error) = SYS_GETRLIMIT

func Getrlimit(resource int, rlim *Rlimit) (err error) {
	err = prlimit(0, resource, nil, rlim)
	if err != ENOSYS {
		return err
	}

	rl := rlimit32{}
	err = getrlimit(resource, &rl)
	if err != nil {
		return
	}

	if rl.Cur == rlimInf32 {
		rlim.Cur = rlimInf64
	} else {
		rlim.Cur = uint64(rl.Cur)
	}

	if rl.Max == rlimInf32 {
		rlim.Max = rlimInf64
	} else {
		rlim.Max = uint64(rl.Max)
	}
	return
}

//sysnb setrlimit1(resource int, rlim *rlimit32) (err error) = SYS_SETRLIMIT

func setrlimit(resource int, rlim *Rlimit) (err error) {
	err = prlimit(0, resource, rlim, nil)
	if err != ENOSYS {
		return err
	}

	rl := rlimit32{}
	if rlim.Cur == rlimInf64 {
		rl.Cur = rlimInf32
	} else if rlim.Cur < uint64(rlimInf32) {
		rl.Cur = uint32(rlim.Cur)
	} else {
		return EINVAL
	}
	if rlim.Max == rlimInf64 {
		rl.Max = rlimInf32
	} else if rlim.Max < uint64(rlimInf32) {
		rl.Max = uint32(rlim.Max)
	} else {
		return EINVAL
	}

	return setrlimit1(resource, &rl)
}

//go:nosplit
func rawSetrlimit(resource int, rlim *Rlimit) Errno {
	_, _, errno := RawSyscall6(SYS_PRLIMIT64, 0, uintptr(resource), uintptr(unsafe.Pointer(rlim)), 0, 0, 0)
	if errno != ENOSYS {
		return errno
	}

	rl := rlimit32{}
	if rlim.Cur == rlimInf64 {
		rl.Cur = rlimInf32
	} else if rlim.Cur < uint64(rlimInf32) {
		rl.Cur = uint32(rlim.Cur)
	} else {
		return EINVAL
	}
	if rlim.Max == rlimInf64 {
		rl.Max = rlimInf32
	} else if rlim.Max < uint64(rlimInf32) {
		rl.Max = uint32(rlim.Max)
	} else {
		return EINVAL
	}

	_, _, errno = RawSyscall(SYS_SETRLIMIT, uintptr(resource), uintptr(unsafe.Pointer(rlim)), 0)
	return errno
}

func (r *PtraceRegs) PC() uint64 { return uint64(r.Regs[64]) }

func (r *PtraceRegs) SetPC(pc uint64) { r.Regs[64] = uint32(pc) }

func (iov *Iovec) SetLen(length int) {
	iov.Len = uint32(length)
}

func (msghdr *Msghdr) SetControllen(length int) {
	msghdr.Controllen = uint32(length)
}

func (cmsg *Cmsghdr) SetLen(length int) {
	cmsg.Len = uint32(length)
>>>>>>> /tmp/go121/src/./syscall/syscall_linux_mipsx.go
}
