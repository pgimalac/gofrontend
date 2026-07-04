// syscall_linux_s390x.go -- GNU/Linux s390x specific support

// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The PtraceRegs struct generated for go looks like this:
//
// type PtraceRegs struct
// {
//   Psw _psw_t;
//   Gprs [15+1]uint64;
//   Acrs [15+1]uint32;
//   Orig_gpr2 uint64;
//   Fp_regs _s390_fp_regs;
//   Per_info _per_struct;
//   Ieee_instruction_pointer uint64;
// }
//
// The GETREGSET/SETREGSET ptrace commands on S/390 only read/write
// the content up to Orig_gpr2.  Hence, we use
// PEEKUSR_AREA/POKEUSR_AREA like GDB does.

package syscall

import "unsafe"

<<<<<<< go/./syscall/syscall_linux_s390x.go
func (r *PtraceRegs) PC() uint64 { return r.Psw.addr }
=======
const (
	_SYS_setgroups  = SYS_SETGROUPS
	_SYS_clone3     = 435
	_SYS_faccessat2 = 439
	_SYS_fchmodat2  = 452
)

//sys	Dup2(oldfd int, newfd int) (err error)
//sys	EpollWait(epfd int, events []EpollEvent, msec int) (n int, err error)
//sys	Fchown(fd int, uid int, gid int) (err error)
//sys	Fstat(fd int, stat *Stat_t) (err error)
//sys	fstatat(dirfd int, path string, stat *Stat_t, flags int) (err error) = SYS_NEWFSTATAT
//sys	Fstatfs(fd int, buf *Statfs_t) (err error)
//sys	Ftruncate(fd int, length int64) (err error)
//sysnb	Getegid() (egid int)
//sysnb	Geteuid() (euid int)
//sysnb	Getgid() (gid int)
//sysnb	Getrlimit(resource int, rlim *Rlimit) (err error) = SYS_GETRLIMIT
//sysnb	Getuid() (uid int)
//sysnb	InotifyInit() (fd int, err error)
//sys	Lchown(path string, uid int, gid int) (err error)
//sys	Lstat(path string, stat *Stat_t) (err error)
//sys	Pause() (err error)
//sys	pread(fd int, p []byte, offset int64) (n int, err error) = SYS_PREAD64
//sys	pwrite(fd int, p []byte, offset int64) (n int, err error) = SYS_PWRITE64
//sys	Renameat(olddirfd int, oldpath string, newdirfd int, newpath string) (err error)
//sys	Seek(fd int, offset int64, whence int) (off int64, err error) = SYS_LSEEK
//sys	Select(nfd int, r *FdSet, w *FdSet, e *FdSet, timeout *Timeval) (n int, err error)
//sys	sendfile(outfd int, infd int, offset *int64, count int) (written int, err error)
//sys	Setfsgid(gid int) (err error)
//sys	Setfsuid(uid int) (err error)
//sysnb	setrlimit(resource int, rlim *Rlimit) (err error) = SYS_SETRLIMIT
//sys	Splice(rfd int, roff *int64, wfd int, woff *int64, len int, flags int) (n int64, err error)
//sys	Stat(path string, stat *Stat_t) (err error)
//sys	Statfs(path string, buf *Statfs_t) (err error)
//sys	SyncFileRange(fd int, off int64, n int64, flags int) (err error) = SYS_SYNC_FILE_RANGE
//sys	Truncate(path string, length int64) (err error)
//sys	Ustat(dev int, ubuf *Ustat_t) (err error)
//sysnb	getgroups(n int, list *_Gid_t) (nn int, err error)

//sys	futimesat(dirfd int, path string, times *[2]Timeval) (err error)
//sysnb	Gettimeofday(tv *Timeval) (err error)

func Time(t *Time_t) (tt Time_t, err error) {
	var tv Timeval
	err = Gettimeofday(&tv)
	if err != nil {
		return 0, err
	}
	if t != nil {
		*t = Time_t(tv.Sec)
	}
	return Time_t(tv.Sec), nil
}

//sys	Utime(path string, buf *Utimbuf) (err error)
//sys	utimes(path string, times *[2]Timeval) (err error)

func setTimespec(sec, nsec int64) Timespec {
	return Timespec{Sec: sec, Nsec: nsec}
}

func setTimeval(sec, usec int64) Timeval {
	return Timeval{Sec: sec, Usec: usec}
}

// Linux on s390x uses the old mmap interface, which requires arguments to be passed in a struct.
// mmap2 also requires arguments to be passed in a struct; it is currently not exposed in <asm/unistd.h>.
func mmap(addr uintptr, length uintptr, prot int, flags int, fd int, offset int64) (xaddr uintptr, err error) {
	mmap_args := [6]uintptr{addr, length, uintptr(prot), uintptr(flags), uintptr(fd), uintptr(offset)}
	r0, _, e1 := Syscall(SYS_MMAP, uintptr(unsafe.Pointer(&mmap_args[0])), 0, 0)
	xaddr = uintptr(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}

// On s390x Linux, all the socket calls go through an extra indirection.
// The arguments to the underlying system call are the number below
// and a pointer to an array of uintptr.  We hide the pointer in the
// socketcall assembly to avoid allocation on every system call.

const (
	// see linux/net.h
	_SOCKET      = 1
	_BIND        = 2
	_CONNECT     = 3
	_LISTEN      = 4
	_ACCEPT      = 5
	_GETSOCKNAME = 6
	_GETPEERNAME = 7
	_SOCKETPAIR  = 8
	_SEND        = 9
	_RECV        = 10
	_SENDTO      = 11
	_RECVFROM    = 12
	_SHUTDOWN    = 13
	_SETSOCKOPT  = 14
	_GETSOCKOPT  = 15
	_SENDMSG     = 16
	_RECVMSG     = 17
	_ACCEPT4     = 18
	_RECVMMSG    = 19
	_SENDMMSG    = 20
)
>>>>>>> /tmp/go122/src/./syscall/syscall_linux_s390x.go

func (r *PtraceRegs) SetPC(pc uint64) { r.Psw.addr = pc }

func PtraceGetRegs(pid int, regs *PtraceRegs) (err error) {
	parea := _ptrace_area{
		_sizeof_ptrace_area,
		0,
		uint64(uintptr(unsafe.Pointer(regs))),
	}
	return ptrace(PTRACE_PEEKUSR_AREA, pid, uintptr(unsafe.Pointer(&parea)), 0)
}

func PtraceSetRegs(pid int, regs *PtraceRegs) (err error) {
	parea := _ptrace_area{
		_sizeof_ptrace_area,
		0,
		uint64(uintptr(unsafe.Pointer(regs))),
	}
	return ptrace(PTRACE_POKEUSR_AREA, pid, uintptr(unsafe.Pointer(&parea)), 0)
}
