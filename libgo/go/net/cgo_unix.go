// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build cgo && !netgo && (aix || darwin || dragonfly || freebsd || hurd || linux || netbsd || openbsd || solaris)

package net

/*
#include <sys/types.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <netdb.h>
#include <unistd.h>
#include <string.h>

// If nothing else defined EAI_OVERFLOW, make sure it has a value.
#ifndef EAI_OVERFLOW
#define EAI_OVERFLOW -12
#endif
*/

import (
	"context"
<<<<<<< go/./net/cgo_unix.go
=======
	"errors"
	"net/netip"
>>>>>>> /tmp/go121/src/./net/cgo_unix.go
	"syscall"
	"unsafe"
)

//extern getaddrinfo
func libc_getaddrinfo(node *byte, service *byte, hints *syscall.Addrinfo, res **syscall.Addrinfo) int32

//extern freeaddrinfo
func libc_freeaddrinfo(res *syscall.Addrinfo)

//extern gai_strerror
func libc_gai_strerror(errcode int) *byte

// bytePtrToString takes a NUL-terminated array of bytes and convert
// it to a Go string.
func bytePtrToString(p *byte) string {
	a := (*[10000]byte)(unsafe.Pointer(p))
	i := 0
	for a[i] != 0 {
		i++
	}
	return string(a[:i])
}

// cgoAvailable set to true to indicate that the cgo resolver
// is available on this system.
const cgoAvailable = true

// An addrinfoErrno represents a getaddrinfo, getnameinfo-specific
// error number. It's a signed number and a zero value is a non-error
// by convention.
type addrinfoErrno int

func (eai addrinfoErrno) Error() string   { return bytePtrToString(libc_gai_strerror(int(eai))) }
func (eai addrinfoErrno) Temporary() bool { return eai == syscall.EAI_AGAIN }
func (eai addrinfoErrno) Timeout() bool   { return false }

// isAddrinfoErrno is just for testing purposes.
func (eai addrinfoErrno) isAddrinfoErrno() {}

// doBlockingWithCtx executes a blocking function in a separate goroutine when the provided
// context is cancellable. It is intended for use with calls that don't support context
// cancellation (cgo, syscalls). blocking func may still be running after this function finishes.
func doBlockingWithCtx[T any](ctx context.Context, blocking func() (T, error)) (T, error) {
	if ctx.Done() == nil {
		return blocking()
	}

	type result struct {
		res T
		err error
	}

	res := make(chan result, 1)
	go func() {
		var r result
		r.res, r.err = blocking()
		res <- r
	}()

	select {
	case r := <-res:
		return r.res, r.err
	case <-ctx.Done():
		var zero T
		return zero, mapErr(ctx.Err())
	}
}

func cgoLookupHost(ctx context.Context, name string) (hosts []string, err error) {
	addrs, err := cgoLookupIP(ctx, "ip", name)
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		hosts = append(hosts, addr.String())
	}
	return hosts, nil
}

<<<<<<< go/./net/cgo_unix.go
func cgoLookupPort(ctx context.Context, network, service string) (port int, err error, completed bool) {
	var hints syscall.Addrinfo
=======
func cgoLookupPort(ctx context.Context, network, service string) (port int, err error) {
	var hints _C_struct_addrinfo
>>>>>>> /tmp/go121/src/./net/cgo_unix.go
	switch network {
	case "": // no hints
	case "tcp", "tcp4", "tcp6":
		hints.Ai_socktype = syscall.SOCK_STREAM
		hints.Ai_protocol = syscall.IPPROTO_TCP
	case "udp", "udp4", "udp6":
		hints.Ai_socktype = syscall.SOCK_DGRAM
		hints.Ai_protocol = syscall.IPPROTO_UDP
	default:
		return 0, &DNSError{Err: "unknown network", Name: network + "/" + service}
	}
	switch ipVersion(network) {
	case '4':
		hints.Ai_family = syscall.AF_INET
	case '6':
		hints.Ai_family = syscall.AF_INET6
	}

	return doBlockingWithCtx(ctx, func() (int, error) {
		return cgoLookupServicePort(&hints, network, service)
	})
}

<<<<<<< go/./net/cgo_unix.go
func cgoLookupServicePort(hints *syscall.Addrinfo, network, service string) (port int, err error) {
	s, err := syscall.BytePtrFromString(service)
	if err != nil {
		return 0, err
=======
func cgoLookupServicePort(hints *_C_struct_addrinfo, network, service string) (port int, err error) {
	cservice, err := syscall.ByteSliceFromString(service)
	if err != nil {
		return 0, &DNSError{Err: err.Error(), Name: network + "/" + service}
	}
	// Lowercase the C service name.
	for i, b := range cservice[:len(service)] {
		cservice[i] = lowerASCII(b)
>>>>>>> /tmp/go121/src/./net/cgo_unix.go
	}
	// Lowercase the service name in the memory passed to C.
	for i := 0; i < len(service); i++ {
		bp := (*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(s)) + uintptr(i)))
		*bp = lowerASCII(*bp)
	}
	var res *syscall.Addrinfo
	syscall.Entersyscall()
	gerrno := libc_getaddrinfo(nil, s, hints, &res)
	syscall.Exitsyscall()
	if gerrno != 0 {
		isTemporary := false
		switch gerrno {
		case syscall.EAI_SYSTEM:
			errno := syscall.GetErrno()
			if errno == 0 { // see golang.org/issue/6232
				errno = syscall.EMFILE
			}
			err = errno
		default:
			err = addrinfoErrno(gerrno)
			isTemporary = addrinfoErrno(gerrno).Temporary()
		}
		return 0, &DNSError{Err: err.Error(), Name: network + "/" + service, IsTemporary: isTemporary}
	}
	defer libc_freeaddrinfo(res)

	for r := res; r != nil; r = r.Ai_next {
		switch r.Ai_family {
		case syscall.AF_INET:
			sa := (*syscall.RawSockaddrInet4)(unsafe.Pointer(r.Ai_addr))
			p := (*[2]byte)(unsafe.Pointer(&sa.Port))
			return int(p[0])<<8 | int(p[1]), nil
		case syscall.AF_INET6:
			sa := (*syscall.RawSockaddrInet6)(unsafe.Pointer(r.Ai_addr))
			p := (*[2]byte)(unsafe.Pointer(&sa.Port))
			return int(p[0])<<8 | int(p[1]), nil
		}
	}
	return 0, &DNSError{Err: "unknown port", Name: network + "/" + service}
}

<<<<<<< go/./net/cgo_unix.go
func cgoPortLookup(result chan<- portLookupResult, hints *syscall.Addrinfo, network, service string) {
	port, err := cgoLookupServicePort(hints, network, service)
	result <- portLookupResult{port, err}
}

func cgoLookupIPCNAME(network, name string) (addrs []IPAddr, cname string, err error) {
=======
func cgoLookupHostIP(network, name string) (addrs []IPAddr, err error) {
>>>>>>> /tmp/go121/src/./net/cgo_unix.go
	acquireThread()
	defer releaseThread()

	var hints syscall.Addrinfo
	hints.Ai_flags = int32(cgoAddrInfoFlags)
	hints.Ai_socktype = syscall.SOCK_STREAM
	hints.Ai_family = syscall.AF_UNSPEC
	switch ipVersion(network) {
	case '4':
		hints.Ai_family = syscall.AF_INET
	case '6':
		hints.Ai_family = syscall.AF_INET6
	}

<<<<<<< go/./net/cgo_unix.go
	h := syscall.StringBytePtr(name)
	var res *syscall.Addrinfo
	syscall.Entersyscall()
	gerrno := libc_getaddrinfo(h, nil, &hints, &res)
	syscall.Exitsyscall()
=======
	h, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, &DNSError{Err: err.Error(), Name: name}
	}
	var res *_C_struct_addrinfo
	gerrno, err := _C_getaddrinfo((*_C_char)(unsafe.Pointer(h)), nil, &hints, &res)
>>>>>>> /tmp/go121/src/./net/cgo_unix.go
	if gerrno != 0 {
		isErrorNoSuchHost := false
		isTemporary := false
		switch gerrno {
		case syscall.EAI_SYSTEM:
			errno := syscall.GetErrno()
			if errno == 0 {
				// err should not be nil, but sometimes getaddrinfo returns
				// gerrno == C.EAI_SYSTEM with err == nil on Linux.
				// The report claims that it happens when we have too many
				// open files, so use syscall.EMFILE (too many open files in system).
				// Most system calls would return ENFILE (too many open files),
				// so at the least EMFILE should be easy to recognize if this
				// comes up again. golang.org/issue/6232.
				errno = syscall.EMFILE
			}
<<<<<<< go/./net/cgo_unix.go
			err = errno
		case syscall.EAI_NONAME:
=======
		case _C_EAI_NONAME, _C_EAI_NODATA:
>>>>>>> /tmp/go121/src/./net/cgo_unix.go
			err = errNoSuchHost
			isErrorNoSuchHost = true
		default:
			err = addrinfoErrno(gerrno)
			isTemporary = addrinfoErrno(gerrno).Temporary()
		}

		return nil, &DNSError{Err: err.Error(), Name: name, IsNotFound: isErrorNoSuchHost, IsTemporary: isTemporary}
	}
	defer libc_freeaddrinfo(res)

<<<<<<< go/./net/cgo_unix.go
	if res != nil {
		cname = bytePtrToString((*byte)(unsafe.Pointer(res.Ai_canonname)))
		if cname == "" {
			cname = name
		}
		if len(cname) > 0 && cname[len(cname)-1] != '.' {
			cname += "."
		}
	}
	for r := res; r != nil; r = r.Ai_next {
=======
	for r := res; r != nil; r = *_C_ai_next(r) {
>>>>>>> /tmp/go121/src/./net/cgo_unix.go
		// We only asked for SOCK_STREAM, but check anyhow.
		if r.Ai_socktype != syscall.SOCK_STREAM {
			continue
		}
		switch r.Ai_family {
		case syscall.AF_INET:
			sa := (*syscall.RawSockaddrInet4)(unsafe.Pointer(r.Ai_addr))
			addr := IPAddr{IP: copyIP(sa.Addr[:])}
			addrs = append(addrs, addr)
		case syscall.AF_INET6:
			sa := (*syscall.RawSockaddrInet6)(unsafe.Pointer(r.Ai_addr))
			addr := IPAddr{IP: copyIP(sa.Addr[:]), Zone: zoneCache.name(int(sa.Scope_id))}
			addrs = append(addrs, addr)
		}
	}
	return addrs, nil
}

func cgoLookupIP(ctx context.Context, network, name string) (addrs []IPAddr, err error) {
	return doBlockingWithCtx(ctx, func() ([]IPAddr, error) {
		return cgoLookupHostIP(network, name)
	})
}

func cgoLookupCNAME(ctx context.Context, name string) (cname string, err error, completed bool) {
	if ctx.Done() == nil {
		_, cname, err = cgoLookupIPCNAME("ip", name)
		return cname, err, true
	}
	result := make(chan ipLookupResult, 1)
	go cgoIPLookup(result, "ip", name)
	select {
	case r := <-result:
		return r.cname, r.err, true
	case <-ctx.Done():
		return "", mapErr(ctx.Err()), false
	}
}

// These are roughly enough for the following:
//
//	 Source		Encoding			Maximum length of single name entry
//	 Unicast DNS		ASCII or			<=253 + a NUL terminator
//				Unicode in RFC 5892		252 * total number of labels + delimiters + a NUL terminator
//	 Multicast DNS	UTF-8 in RFC 5198 or		<=253 + a NUL terminator
//				the same as unicast DNS ASCII	<=253 + a NUL terminator
//	 Local database	various				depends on implementation
const (
	nameinfoLen    = 64
	maxNameinfoLen = 4096
)

func cgoLookupPTR(ctx context.Context, addr string) (names []string, err error) {
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return nil, &DNSError{Err: "invalid address", Name: addr}
	}
	sa, salen := cgoSockaddr(IP(ip.AsSlice()), ip.Zone())
	if sa == nil {
		return nil, &DNSError{Err: "invalid address " + ip.String(), Name: addr}
	}

	return doBlockingWithCtx(ctx, func() ([]string, error) {
		return cgoLookupAddrPTR(addr, sa, salen)
	})
}

func cgoLookupAddrPTR(addr string, sa *syscall.RawSockaddr, salen syscall.Socklen_t) (names []string, err error) {
	acquireThread()
	defer releaseThread()

	var gerrno int
	var b []byte
	for l := nameinfoLen; l <= maxNameinfoLen; l *= 2 {
		b = make([]byte, l)
		gerrno, err = cgoNameinfoPTR(b, sa, salen)
		if gerrno == 0 || gerrno != syscall.EAI_OVERFLOW {
			break
		}
	}
	if gerrno != 0 {
		isErrorNoSuchHost := false
		isTemporary := false
		switch gerrno {
		case syscall.EAI_SYSTEM:
			if err == nil { // see golang.org/issue/6232
				err = syscall.EMFILE
			}
		case _C_EAI_NONAME:
			err = errNoSuchHost
			isErrorNoSuchHost = true
		default:
			err = addrinfoErrno(gerrno)
			isTemporary = addrinfoErrno(gerrno).Temporary()
		}
		return nil, &DNSError{Err: err.Error(), Name: addr, IsTemporary: isTemporary, IsNotFound: isErrorNoSuchHost}
	}
	for i := 0; i < len(b); i++ {
		if b[i] == 0 {
			b = b[:i]
			break
		}
	}
	return []string{absDomainName(string(b))}, nil
}

<<<<<<< go/./net/cgo_unix.go
func cgoReverseLookup(result chan<- reverseLookupResult, addr string, sa *syscall.RawSockaddr, salen syscall.Socklen_t) {
	names, err := cgoLookupAddrPTR(addr, sa, salen)
	result <- reverseLookupResult{names, err}
}

func cgoSockaddr(ip IP, zone string) (*syscall.RawSockaddr, syscall.Socklen_t) {
=======
func cgoSockaddr(ip IP, zone string) (*_C_struct_sockaddr, _C_socklen_t) {
>>>>>>> /tmp/go121/src/./net/cgo_unix.go
	if ip4 := ip.To4(); ip4 != nil {
		return cgoSockaddrInet4(ip4), syscall.Socklen_t(syscall.SizeofSockaddrInet4)
	}
	if ip6 := ip.To16(); ip6 != nil {
		return cgoSockaddrInet6(ip6, zoneCache.index(zone)), syscall.Socklen_t(syscall.SizeofSockaddrInet6)
	}
	return nil, 0
}
<<<<<<< go/./net/cgo_unix.go
=======

func cgoLookupCNAME(ctx context.Context, name string) (cname string, err error, completed bool) {
	resources, err := resSearch(ctx, name, int(dnsmessage.TypeCNAME), int(dnsmessage.ClassINET))
	if err != nil {
		return
	}
	cname, err = parseCNAMEFromResources(resources)
	if err != nil {
		return "", err, false
	}
	return cname, nil, true
}

// resSearch will make a call to the 'res_nsearch' routine in the C library
// and parse the output as a slice of DNS resources.
func resSearch(ctx context.Context, hostname string, rtype, class int) ([]dnsmessage.Resource, error) {
	return doBlockingWithCtx(ctx, func() ([]dnsmessage.Resource, error) {
		return cgoResSearch(hostname, rtype, class)
	})
}

func cgoResSearch(hostname string, rtype, class int) ([]dnsmessage.Resource, error) {
	acquireThread()
	defer releaseThread()

	state := (*_C_struct___res_state)(_C_malloc(unsafe.Sizeof(_C_struct___res_state{})))
	defer _C_free(unsafe.Pointer(state))
	if err := _C_res_ninit(state); err != nil {
		return nil, errors.New("res_ninit failure: " + err.Error())
	}
	defer _C_res_nclose(state)

	// Some res_nsearch implementations (like macOS) do not set errno.
	// They set h_errno, which is not per-thread and useless to us.
	// res_nsearch returns the size of the DNS response packet.
	// But if the DNS response packet contains failure-like response codes,
	// res_search returns -1 even though it has copied the packet into buf,
	// giving us no way to find out how big the packet is.
	// For now, we are willing to take res_search's word that there's nothing
	// useful in the response, even though there *is* a response.
	bufSize := maxDNSPacketSize
	buf := (*_C_uchar)(_C_malloc(uintptr(bufSize)))
	defer _C_free(unsafe.Pointer(buf))

	s, err := syscall.BytePtrFromString(hostname)
	if err != nil {
		return nil, err
	}

	var size int
	for {
		size, _ = _C_res_nsearch(state, (*_C_char)(unsafe.Pointer(s)), class, rtype, buf, bufSize)
		if size <= 0 || size > 0xffff {
			return nil, errors.New("res_nsearch failure")
		}
		if size <= bufSize {
			break
		}

		// Allocate a bigger buffer to fit the entire msg.
		_C_free(unsafe.Pointer(buf))
		bufSize = size
		buf = (*_C_uchar)(_C_malloc(uintptr(bufSize)))
	}

	var p dnsmessage.Parser
	if _, err := p.Start(unsafe.Slice((*byte)(unsafe.Pointer(buf)), size)); err != nil {
		return nil, err
	}
	p.SkipAllQuestions()
	resources, err := p.AllAnswers()
	if err != nil {
		return nil, err
	}
	return resources, nil
}
>>>>>>> /tmp/go121/src/./net/cgo_unix.go
