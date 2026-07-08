/* atomic.c -- implement atomic routines for Go.

   Copyright 2011 The Go Authors. All rights reserved.
   Use of this source code is governed by a BSD-style
   license that can be found in the LICENSE file.  */

#include <stdint.h>

#include "runtime.h"

extern void panicUnaligned(void)
  __asm__ (GOSYM_PREFIX "internal_1runtime_1atomic.panicUnaligned")
  __attribute__ ((noreturn));

int32_t SwapInt32 (int32_t *, int32_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.SwapInt32")
  __attribute__ ((no_split_stack));

int32_t
SwapInt32 (int32_t *addr, int32_t new)
{
  return __atomic_exchange_n (addr, new, __ATOMIC_SEQ_CST);
}

int64_t SwapInt64 (int64_t *, int64_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.SwapInt64")
  __attribute__ ((no_split_stack));

int64_t
SwapInt64 (int64_t *addr, int64_t new)
{
  if (((uintptr_t) addr & 7) != 0)
    panicUnaligned ();
  return __atomic_exchange_n (addr, new, __ATOMIC_SEQ_CST);
}

uint32_t SwapUint32 (uint32_t *, uint32_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.SwapUint32")
  __attribute__ ((no_split_stack));

uint32_t
SwapUint32 (uint32_t *addr, uint32_t new)
{
  return __atomic_exchange_n (addr, new, __ATOMIC_SEQ_CST);
}

uint64_t SwapUint64 (uint64_t *, uint64_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.SwapUint64")
  __attribute__ ((no_split_stack));

uint64_t
SwapUint64 (uint64_t *addr, uint64_t new)
{
  if (((uintptr_t) addr & 7) != 0)
    panicUnaligned ();
  return __atomic_exchange_n (addr, new, __ATOMIC_SEQ_CST);
}

uintptr_t SwapUintptr (uintptr_t *, uintptr_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.SwapUintptr")
  __attribute__ ((no_split_stack));

uintptr_t
SwapUintptr (uintptr_t *addr, uintptr_t new)
{
  return __atomic_exchange_n (addr, new, __ATOMIC_SEQ_CST);
}

_Bool CompareAndSwapInt32 (int32_t *, int32_t, int32_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.CompareAndSwapInt32")
  __attribute__ ((no_split_stack));

_Bool
CompareAndSwapInt32 (int32_t *val, int32_t old, int32_t new)
{
  return __atomic_compare_exchange_n (val, &old, new, false, __ATOMIC_SEQ_CST,
				      __ATOMIC_RELAXED);
}

_Bool CompareAndSwapInt64 (int64_t *, int64_t, int64_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.CompareAndSwapInt64")
  __attribute__ ((no_split_stack));

_Bool
CompareAndSwapInt64 (int64_t *val, int64_t old, int64_t new)
{
  if (((uintptr_t) val & 7) != 0)
    panicUnaligned ();
  return __atomic_compare_exchange_n (val, &old, new, false, __ATOMIC_SEQ_CST,
				      __ATOMIC_RELAXED);
}

_Bool CompareAndSwapUint32 (uint32_t *, uint32_t, uint32_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.CompareAndSwapUint32")
  __attribute__ ((no_split_stack));

_Bool
CompareAndSwapUint32 (uint32_t *val, uint32_t old, uint32_t new)
{
  return __atomic_compare_exchange_n (val, &old, new, false, __ATOMIC_SEQ_CST,
				      __ATOMIC_RELAXED);
}

_Bool CompareAndSwapUint64 (uint64_t *, uint64_t, uint64_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.CompareAndSwapUint64")
  __attribute__ ((no_split_stack));

_Bool
CompareAndSwapUint64 (uint64_t *val, uint64_t old, uint64_t new)
{
  if (((uintptr_t) val & 7) != 0)
    panicUnaligned ();
  return __atomic_compare_exchange_n (val, &old, new, false, __ATOMIC_SEQ_CST,
				      __ATOMIC_RELAXED);
}

_Bool CompareAndSwapUintptr (uintptr_t *, uintptr_t, uintptr_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.CompareAndSwapUintptr")
  __attribute__ ((no_split_stack));

_Bool
CompareAndSwapUintptr (uintptr_t *val, uintptr_t old, uintptr_t new)
{
  return __atomic_compare_exchange_n (val, &old, new, false, __ATOMIC_SEQ_CST,
				      __ATOMIC_RELAXED);
}

int32_t AddInt32 (int32_t *, int32_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.AddInt32")
  __attribute__ ((no_split_stack));

int32_t
AddInt32 (int32_t *val, int32_t delta)
{
  return __atomic_add_fetch (val, delta, __ATOMIC_SEQ_CST);
}

uint32_t AddUint32 (uint32_t *, uint32_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.AddUint32")
  __attribute__ ((no_split_stack));

uint32_t
AddUint32 (uint32_t *val, uint32_t delta)
{
  return __atomic_add_fetch (val, delta, __ATOMIC_SEQ_CST);
}

int64_t AddInt64 (int64_t *, int64_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.AddInt64")
  __attribute__ ((no_split_stack));

int64_t
AddInt64 (int64_t *val, int64_t delta)
{
  if (((uintptr_t) val & 7) != 0)
    panicUnaligned ();
  return __atomic_add_fetch (val, delta, __ATOMIC_SEQ_CST);
}

uint64_t AddUint64 (uint64_t *, uint64_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.AddUint64")
  __attribute__ ((no_split_stack));

uint64_t
AddUint64 (uint64_t *val, uint64_t delta)
{
  if (((uintptr_t) val & 7) != 0)
    panicUnaligned ();
  return __atomic_add_fetch (val, delta, __ATOMIC_SEQ_CST);
}

uintptr_t AddUintptr (uintptr_t *, uintptr_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.AddUintptr")
  __attribute__ ((no_split_stack));

uintptr_t
AddUintptr (uintptr_t *val, uintptr_t delta)
{
  return __atomic_add_fetch (val, delta, __ATOMIC_SEQ_CST);
}

int32_t LoadInt32 (int32_t *addr)
  __asm__ (GOSYM_PREFIX "sync_1atomic.LoadInt32")
  __attribute__ ((no_split_stack));

int32_t
LoadInt32 (int32_t *addr)
{
  return __atomic_load_n (addr, __ATOMIC_SEQ_CST);
}

int64_t LoadInt64 (int64_t *addr)
  __asm__ (GOSYM_PREFIX "sync_1atomic.LoadInt64")
  __attribute__ ((no_split_stack));

int64_t
LoadInt64 (int64_t *addr)
{
  if (((uintptr_t) addr & 7) != 0)
    panicUnaligned ();
  return __atomic_load_n (addr, __ATOMIC_SEQ_CST);
}

uint32_t LoadUint32 (uint32_t *addr)
  __asm__ (GOSYM_PREFIX "sync_1atomic.LoadUint32")
  __attribute__ ((no_split_stack));

uint32_t
LoadUint32 (uint32_t *addr)
{
  return __atomic_load_n (addr, __ATOMIC_SEQ_CST);
}

uint64_t LoadUint64 (uint64_t *addr)
  __asm__ (GOSYM_PREFIX "sync_1atomic.LoadUint64")
  __attribute__ ((no_split_stack));

uint64_t
LoadUint64 (uint64_t *addr)
{
  if (((uintptr_t) addr & 7) != 0)
    panicUnaligned ();
  return __atomic_load_n (addr, __ATOMIC_SEQ_CST);
}

uintptr_t LoadUintptr (uintptr_t *addr)
  __asm__ (GOSYM_PREFIX "sync_1atomic.LoadUintptr")
  __attribute__ ((no_split_stack));

uintptr_t
LoadUintptr (uintptr_t *addr)
{
  return __atomic_load_n (addr, __ATOMIC_SEQ_CST);
}

void *LoadPointer (void **addr)
  __asm__ (GOSYM_PREFIX "sync_1atomic.LoadPointer")
  __attribute__ ((no_split_stack));

void *
LoadPointer (void **addr)
{
  return __atomic_load_n (addr, __ATOMIC_SEQ_CST);
}

void StoreInt32 (int32_t *addr, int32_t val)
  __asm__ (GOSYM_PREFIX "sync_1atomic.StoreInt32")
  __attribute__ ((no_split_stack));

void
StoreInt32 (int32_t *addr, int32_t val)
{
  __atomic_store_n (addr, val, __ATOMIC_SEQ_CST);
}

void StoreInt64 (int64_t *addr, int64_t val)
  __asm__ (GOSYM_PREFIX "sync_1atomic.StoreInt64")
  __attribute__ ((no_split_stack));

void
StoreInt64 (int64_t *addr, int64_t val)
{
  if (((uintptr_t) addr & 7) != 0)
    panicUnaligned ();
  __atomic_store_n (addr, val, __ATOMIC_SEQ_CST);
}

void StoreUint32 (uint32_t *addr, uint32_t val)
  __asm__ (GOSYM_PREFIX "sync_1atomic.StoreUint32")
  __attribute__ ((no_split_stack));

void
StoreUint32 (uint32_t *addr, uint32_t val)
{
  __atomic_store_n (addr, val, __ATOMIC_SEQ_CST);
}

void StoreUint64 (uint64_t *addr, uint64_t val)
  __asm__ (GOSYM_PREFIX "sync_1atomic.StoreUint64")
  __attribute__ ((no_split_stack));

void
StoreUint64 (uint64_t *addr, uint64_t val)
{
  if (((uintptr_t) addr & 7) != 0)
    panicUnaligned ();
  __atomic_store_n (addr, val, __ATOMIC_SEQ_CST);
}

void StoreUintptr (uintptr_t *addr, uintptr_t val)
  __asm__ (GOSYM_PREFIX "sync_1atomic.StoreUintptr")
  __attribute__ ((no_split_stack));

void
StoreUintptr (uintptr_t *addr, uintptr_t val)
{
  __atomic_store_n (addr, val, __ATOMIC_SEQ_CST);
}

int32_t AndInt32 (int32_t *, int32_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.AndInt32")
  __attribute__ ((no_split_stack));

int32_t
AndInt32 (int32_t *addr, int32_t mask)
{
  return __atomic_fetch_and (addr, mask, __ATOMIC_SEQ_CST);
}

uint32_t AndUint32 (uint32_t *, uint32_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.AndUint32")
  __attribute__ ((no_split_stack));

uint32_t
AndUint32 (uint32_t *addr, uint32_t mask)
{
  return __atomic_fetch_and (addr, mask, __ATOMIC_SEQ_CST);
}

int64_t AndInt64 (int64_t *, int64_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.AndInt64")
  __attribute__ ((no_split_stack));

int64_t
AndInt64 (int64_t *addr, int64_t mask)
{
  if (((uintptr_t) addr & 7) != 0)
    panicUnaligned ();
  return __atomic_fetch_and (addr, mask, __ATOMIC_SEQ_CST);
}

uint64_t AndUint64 (uint64_t *, uint64_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.AndUint64")
  __attribute__ ((no_split_stack));

uint64_t
AndUint64 (uint64_t *addr, uint64_t mask)
{
  if (((uintptr_t) addr & 7) != 0)
    panicUnaligned ();
  return __atomic_fetch_and (addr, mask, __ATOMIC_SEQ_CST);
}

uintptr_t AndUintptr (uintptr_t *, uintptr_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.AndUintptr")
  __attribute__ ((no_split_stack));

uintptr_t
AndUintptr (uintptr_t *addr, uintptr_t mask)
{
  return __atomic_fetch_and (addr, mask, __ATOMIC_SEQ_CST);
}

int32_t OrInt32 (int32_t *, int32_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.OrInt32")
  __attribute__ ((no_split_stack));

int32_t
OrInt32 (int32_t *addr, int32_t mask)
{
  return __atomic_fetch_or (addr, mask, __ATOMIC_SEQ_CST);
}

uint32_t OrUint32 (uint32_t *, uint32_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.OrUint32")
  __attribute__ ((no_split_stack));

uint32_t
OrUint32 (uint32_t *addr, uint32_t mask)
{
  return __atomic_fetch_or (addr, mask, __ATOMIC_SEQ_CST);
}

int64_t OrInt64 (int64_t *, int64_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.OrInt64")
  __attribute__ ((no_split_stack));

int64_t
OrInt64 (int64_t *addr, int64_t mask)
{
  if (((uintptr_t) addr & 7) != 0)
    panicUnaligned ();
  return __atomic_fetch_or (addr, mask, __ATOMIC_SEQ_CST);
}

uint64_t OrUint64 (uint64_t *, uint64_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.OrUint64")
  __attribute__ ((no_split_stack));

uint64_t
OrUint64 (uint64_t *addr, uint64_t mask)
{
  if (((uintptr_t) addr & 7) != 0)
    panicUnaligned ();
  return __atomic_fetch_or (addr, mask, __ATOMIC_SEQ_CST);
}

uintptr_t OrUintptr (uintptr_t *, uintptr_t)
  __asm__ (GOSYM_PREFIX "sync_1atomic.OrUintptr")
  __attribute__ ((no_split_stack));

uintptr_t
OrUintptr (uintptr_t *addr, uintptr_t mask)
{
  return __atomic_fetch_or (addr, mask, __ATOMIC_SEQ_CST);
}
