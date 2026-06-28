#!/bin/sh
# Regression tests for the gccgo generics implementation (branch
# pgimalac/generics).  Each *.go file is compiled and run with a
# generics-enabled gccgo; its output is compared against the committed
# golden *.out file (which was produced with the upstream "go" tool).
#
# Usage:
#   ./run.sh [GCC_BUILD_DIR]
#
# GCC_BUILD_DIR defaults to $GCCGO_BUILD or /home/bits/pgimalac/gcc-build.
# It must be a GCC build tree that built this frontend (contains
# gcc/gccgo, gcc/go1 and x86_64-pc-linux-gnu/libgo).
#
# Exit status is non-zero if any test fails.

set -u

BUILD="${1:-${GCCGO_BUILD:-/home/bits/pgimalac/gcc-build}}"
GCCDIR="$BUILD/gcc"
GCCGO="$GCCDIR/gccgo"

# Find the target libgo build directory: the one that actually contains
# the compiled standard-library export data (fmt.gox).
LIBGODIR=""
for d in "$BUILD"/*/libgo; do
  if [ -f "$d/fmt.gox" ]; then
    LIBGODIR="$d"
    break
  fi
done
if [ -z "$LIBGODIR" ]; then
  echo "error: could not find a built libgo (with fmt.gox) under $BUILD" >&2
  exit 2
fi
LIBGO="$LIBGODIR/.libs"

if [ ! -x "$GCCGO" ]; then
  echo "error: gccgo not found at $GCCGO" >&2
  echo "pass the GCC build directory as the first argument" >&2
  exit 2
fi

cd "$(dirname "$0")" || exit 2

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

pass=0
fail=0
for src in *.go; do
  base="${src%.go}"
  exp="$base.out"
  bin="$tmp/$base"
  if ! "$GCCGO" -B"$GCCDIR" -I"$LIBGODIR" -L"$LIBGODIR" -L"$LIBGO" \
       -static-libgo -o "$bin" "$src" > "$tmp/$base.cerr" 2>&1; then
    echo "FAIL  $src (compile)"
    sed 's/^/        /' "$tmp/$base.cerr"
    fail=$((fail + 1))
    continue
  fi
  got="$(LD_LIBRARY_PATH="$LIBGO" "$bin" 2>&1)"
  if [ "$got" = "$(cat "$exp")" ]; then
    echo "PASS  $src"
    pass=$((pass + 1))
  else
    echo "FAIL  $src (output)"
    echo "  --- expected ---"; sed 's/^/  /' "$exp"
    echo "  --- got ---";      printf '%s\n' "$got" | sed 's/^/  /'
    fail=$((fail + 1))
  fi
done

echo "------------------------------------"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
