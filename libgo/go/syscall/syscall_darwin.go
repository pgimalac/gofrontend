// Copyright 2009,2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package syscall

import (
	"internal/abi"
	"unsafe"
)

func direntIno(buf []byte) (uint64, bool) {
	return readInt(buf, unsafe.Offsetof(Dirent{}.Ino), unsafe.Sizeof(Dirent{}.Ino))
}

func direntReclen(buf []byte) (uint64, bool) {
	return readInt(buf, unsafe.Offsetof(Dirent{}.Reclen), unsafe.Sizeof(Dirent{}.Reclen))
}

func direntNamlen(buf []byte) (uint64, bool) {
	return readInt(buf, unsafe.Offsetof(Dirent{}.Namlen), unsafe.Sizeof(Dirent{}.Namlen))
}
<<<<<<< go/./syscall/syscall_darwin.go
=======

func PtraceAttach(pid int) (err error) { return ptrace(PT_ATTACH, pid, 0, 0) }
func PtraceDetach(pid int) (err error) { return ptrace(PT_DETACH, pid, 0, 0) }

//sysnb pipe(p *[2]int32) (err error)

func Pipe(p []int) (err error) {
	if len(p) != 2 {
		return EINVAL
	}
	var q [2]int32
	err = pipe(&q)
	if err == nil {
		p[0] = int(q[0])
		p[1] = int(q[1])
	}
	return
}

func Getfsstat(buf []Statfs_t, flags int) (n int, err error) {
	var _p0 unsafe.Pointer
	var bufsize uintptr
	if len(buf) > 0 {
		_p0 = unsafe.Pointer(&buf[0])
		bufsize = unsafe.Sizeof(Statfs_t{}) * uintptr(len(buf))
	}
	r0, _, e1 := syscall(abi.FuncPCABI0(libc_getfsstat_trampoline), uintptr(_p0), bufsize, uintptr(flags))
	n = int(r0)
	if e1 != 0 {
		err = e1
	}
	return
}

func libc_getfsstat_trampoline()

//go:cgo_import_dynamic libc_getfsstat getfsstat "/usr/lib/libSystem.B.dylib"

//sys	utimensat(dirfd int, path string, times *[2]Timespec, flags int) (err error)

/*
 * Wrapped
 */

//sys	kill(pid int, signum int, posix int) (err error)

func Kill(pid int, signum Signal) (err error) { return kill(pid, int(signum), 1) }

/*
 * Exposed directly
 */
//sys	Access(path string, mode uint32) (err error)
//sys	Adjtime(delta *Timeval, olddelta *Timeval) (err error)
//sys	Chdir(path string) (err error)
//sys	Chflags(path string, flags int) (err error)
//sys	Chmod(path string, mode uint32) (err error)
//sys	Chown(path string, uid int, gid int) (err error)
//sys	Chroot(path string) (err error)
//sys	Close(fd int) (err error)
//sys	closedir(dir uintptr) (err error)
//sys	Dup(fd int) (nfd int, err error)
//sys	Dup2(from int, to int) (err error)
//sys	Exchangedata(path1 string, path2 string, options int) (err error)
//sys	Fchdir(fd int) (err error)
//sys	Fchflags(fd int, flags int) (err error)
//sys	Fchmod(fd int, mode uint32) (err error)
//sys	Fchown(fd int, uid int, gid int) (err error)
//sys	Flock(fd int, how int) (err error)
//sys	Fpathconf(fd int, name int) (val int, err error)
//sys	Fsync(fd int) (err error)
//  Fsync is not called for os.File.Sync(). Please see internal/poll/fd_fsync_darwin.go
//sys	Ftruncate(fd int, length int64) (err error)
//sys	Getdtablesize() (size int)
//sysnb	Getegid() (egid int)
//sysnb	Geteuid() (uid int)
//sysnb	Getgid() (gid int)
//sysnb	Getpgid(pid int) (pgid int, err error)
//sysnb	Getpgrp() (pgrp int)
//sysnb	Getpid() (pid int)
//sysnb	Getppid() (ppid int)
//sys	Getpriority(which int, who int) (prio int, err error)
//sysnb	Getrlimit(which int, lim *Rlimit) (err error)
//sysnb	Getrusage(who int, rusage *Rusage) (err error)
//sysnb	Getsid(pid int) (sid int, err error)
//sysnb	Getuid() (uid int)
//sysnb	Issetugid() (tainted bool)
//sys	Kqueue() (fd int, err error)
//sys	Lchown(path string, uid int, gid int) (err error)
//sys	Link(path string, link string) (err error)
//sys	Listen(s int, backlog int) (err error)
//sys	Mkdir(path string, mode uint32) (err error)
//sys	Mkfifo(path string, mode uint32) (err error)
//sys	Mknod(path string, mode uint32, dev int) (err error)
//sys	Mlock(b []byte) (err error)
//sys	Mlockall(flags int) (err error)
//sys	Mprotect(b []byte, prot int) (err error)
//sys	msync(b []byte, flags int) (err error)
//sys	Munlock(b []byte) (err error)
//sys	Munlockall() (err error)
//sys	Open(path string, mode int, perm uint32) (fd int, err error)
//sys	Pathconf(path string, name int) (val int, err error)
//sys	pread(fd int, p []byte, offset int64) (n int, err error)
//sys	pwrite(fd int, p []byte, offset int64) (n int, err error)
//sys	read(fd int, p []byte) (n int, err error)
//sys	readdir_r(dir uintptr, entry *Dirent, result **Dirent) (res Errno)
//sys	Readlink(path string, buf []byte) (n int, err error)
//sys	Rename(from string, to string) (err error)
//sys	Revoke(path string) (err error)
//sys	Rmdir(path string) (err error)
//sys	Seek(fd int, offset int64, whence int) (newoffset int64, err error) = SYS_lseek
//sys	Select(n int, r *FdSet, w *FdSet, e *FdSet, timeout *Timeval) (err error)
//sys	Setegid(egid int) (err error)
//sysnb	Seteuid(euid int) (err error)
//sysnb	Setgid(gid int) (err error)
//sys	Setlogin(name string) (err error)
//sysnb	Setpgid(pid int, pgid int) (err error)
//sys	Setpriority(which int, who int, prio int) (err error)
//sys	Setprivexec(flag int) (err error)
//sysnb	Setregid(rgid int, egid int) (err error)
//sysnb	Setreuid(ruid int, euid int) (err error)
//sysnb	setrlimit(which int, lim *Rlimit) (err error)
//sysnb	Setsid() (pid int, err error)
//sysnb	Settimeofday(tp *Timeval) (err error)
//sysnb	Setuid(uid int) (err error)
//sys	Symlink(path string, link string) (err error)
//sys	Sync() (err error)
//sys	Truncate(path string, length int64) (err error)
//sys	Umask(newmask int) (oldmask int)
//sys	Undelete(path string) (err error)
//sys	Unlink(path string) (err error)
//sys	Unmount(path string, flags int) (err error)
//sys	write(fd int, p []byte) (n int, err error)
//sys	writev(fd int, iovecs []Iovec) (cnt uintptr, err error)
//sys   mmap(addr uintptr, length uintptr, prot int, flag int, fd int, pos int64) (ret uintptr, err error)
//sys   munmap(addr uintptr, length uintptr) (err error)
//sysnb fork() (pid int, err error)
//sysnb execve(path *byte, argv **byte, envp **byte) (err error)
//sysnb exit(res int) (err error)
//sys	sysctl(mib []_C_int, old *byte, oldlen *uintptr, new *byte, newlen uintptr) (err error)
//sys   unlinkat(fd int, path string, flags int) (err error)
//sys   openat(fd int, path string, flags int, perm uint32) (fdret int, err error)
//sys	getcwd(buf []byte) (n int, err error)

func init() {
	execveDarwin = execve
}

func fdopendir(fd int) (dir uintptr, err error) {
	r0, _, e1 := syscallPtr(abi.FuncPCABI0(libc_fdopendir_trampoline), uintptr(fd), 0, 0)
	dir = r0
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}

func libc_fdopendir_trampoline()

//go:cgo_import_dynamic libc_fdopendir fdopendir "/usr/lib/libSystem.B.dylib"

func readlen(fd int, buf *byte, nbuf int) (n int, err error) {
	r0, _, e1 := syscall(abi.FuncPCABI0(libc_read_trampoline), uintptr(fd), uintptr(unsafe.Pointer(buf)), uintptr(nbuf))
	n = int(r0)
	if e1 != 0 {
		err = errnoErr(e1)
	}
	return
}

func Getdirentries(fd int, buf []byte, basep *uintptr) (n int, err error) {
	// Simulate Getdirentries using fdopendir/readdir_r/closedir.
	// We store the number of entries to skip in the seek
	// offset of fd. See issue #31368.
	// It's not the full required semantics, but should handle the case
	// of calling Getdirentries or ReadDirent repeatedly.
	// It won't handle assigning the results of lseek to *basep, or handle
	// the directory being edited underfoot.
	skip, err := Seek(fd, 0, 1 /* SEEK_CUR */)
	if err != nil {
		return 0, err
	}

	// We need to duplicate the incoming file descriptor
	// because the caller expects to retain control of it, but
	// fdopendir expects to take control of its argument.
	// Just Dup'ing the file descriptor is not enough, as the
	// result shares underlying state. Use openat to make a really
	// new file descriptor referring to the same directory.
	fd2, err := openat(fd, ".", O_RDONLY, 0)
	if err != nil {
		return 0, err
	}
	d, err := fdopendir(fd2)
	if err != nil {
		Close(fd2)
		return 0, err
	}
	defer closedir(d)

	var cnt int64
	for {
		var entry Dirent
		var entryp *Dirent
		e := readdir_r(d, &entry, &entryp)
		if e != 0 {
			return n, errnoErr(e)
		}
		if entryp == nil {
			break
		}
		if skip > 0 {
			skip--
			cnt++
			continue
		}
		reclen := int(entry.Reclen)
		if reclen > len(buf) {
			// Not enough room. Return for now.
			// The counter will let us know where we should start up again.
			// Note: this strategy for suspending in the middle and
			// restarting is O(n^2) in the length of the directory. Oh well.
			break
		}
		// Copy entry into return buffer.
		copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(&entry)), reclen))
		buf = buf[reclen:]
		n += reclen
		cnt++
	}
	// Set the seek offset of the input fd to record
	// how many files we've already returned.
	_, err = Seek(fd, cnt, 0 /* SEEK_SET */)
	if err != nil {
		return n, err
	}

	return n, nil
}

// Implemented in the runtime package (runtime/sys_darwin.go)
func syscall(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno)
func syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err Errno)
func syscall6X(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err Errno)
func rawSyscall(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno)
func rawSyscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err Errno)
func syscallPtr(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno)
>>>>>>> /tmp/go122/src/./syscall/syscall_darwin.go
