#!/bin/sh
# Cross-package generics regression tests for the gccgo frontend.
#
# Each case_<name>/ directory is a small two-package program: a library
# package in case_<name>/lib/lib.go (Go package "lib") and a main package
# in case_<name>/main.go that imports it.  The import path is
# "xtest/case_<name>/lib" (see go.mod), so the same sources are used both
# to generate the golden output with the upstream "go" tool and to test
# the gccgo-built compiler here.
#
# For each case we compile the library with gccgo (producing export data
# that includes the generic templates), extract its .gox, then compile and
# link the main program against it, run it, and compare the output to the
# committed expected.out.
#
# Usage:
#   ./run.sh [GCC_BUILD_DIR]
#
# Exit status is non-zero if any case fails.

set -u

BUILD="${1:-${GCCGO_BUILD:-/home/bits/pgimalac/gcc-build}}"
GCCDIR="$BUILD/gcc"
GCCGO="$GCCDIR/gccgo"

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
  exit 2
fi

cd "$(dirname "$0")" || exit 2
MODULE=xtest

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

run() {
  "$GCCGO" -B"$GCCDIR" -I"$LIBGODIR" -I"$tmp" -L"$LIBGODIR" -L"$LIBGO" "$@"
}

pass=0
fail=0
for dir in case_*/; do
  case="${dir%/}"
  exp="$case/expected.out"
  ip="$MODULE/$case/lib"

  mkdir -p "$tmp/$(dirname "$ip")"
  objs="$tmp/lib.o"

  # An optional deeper package, to exercise transitive generic chains
  # (main -> lib -> base).
  if [ -f "$case/base/base.go" ]; then
    bip="$MODULE/$case/base"
    mkdir -p "$tmp/$(dirname "$bip")"
    if ! run -c -fgo-pkgpath="$bip" "$case/base/base.go" -o "$tmp/base.o" \
         > "$tmp/cerr" 2>&1; then
      echo "FAIL  $case (compile base)"
      sed 's/^/        /' "$tmp/cerr"
      fail=$((fail + 1))
      continue
    fi
    objcopy -j .go_export "$tmp/base.o" "$tmp/$bip.gox" 2>/dev/null
    objs="$objs $tmp/base.o"
  fi

  # Compile the library package; this writes the generic templates into
  # its export data.
  if ! run -c -fgo-pkgpath="$ip" "$case/lib/lib.go" -o "$tmp/lib.o" \
       > "$tmp/cerr" 2>&1; then
    echo "FAIL  $case (compile lib)"
    sed 's/^/        /' "$tmp/cerr"
    fail=$((fail + 1))
    continue
  fi
  objcopy -j .go_export "$tmp/lib.o" "$tmp/$ip.gox" 2>/dev/null

  # A case named case_*_bad is a negative test: the main program must
  # fail to compile (e.g. a cross-package constraint violation).
  case "$case" in
    *_bad)
      if run -static-libgo "$case/main.go" $objs -o "$tmp/prog" \
	   > "$tmp/cerr" 2>&1; then
	echo "FAIL  $case (expected a compile error, but it compiled)"
	fail=$((fail + 1))
      else
	echo "PASS  $case (rejected as expected)"
	pass=$((pass + 1))
      fi
      continue
      ;;
  esac

  # Compile and link the main program against the library.
  if ! run -static-libgo "$case/main.go" $objs -o "$tmp/prog" \
       > "$tmp/cerr" 2>&1; then
    echo "FAIL  $case (compile main)"
    sed 's/^/        /' "$tmp/cerr"
    fail=$((fail + 1))
    continue
  fi

  got="$(LD_LIBRARY_PATH="$LIBGO" "$tmp/prog" 2>&1)"
  if [ "$got" = "$(cat "$exp")" ]; then
    echo "PASS  $case"
    pass=$((pass + 1))
  else
    echo "FAIL  $case (output)"
    echo "  --- expected ---"; sed 's/^/  /' "$exp"
    echo "  --- got ---";      printf '%s\n' "$got" | sed 's/^/  /'
    fail=$((fail + 1))
  fi
done

echo "------------------------------------"
echo "$pass passed, $fail failed"
[ "$fail" -eq 0 ]
