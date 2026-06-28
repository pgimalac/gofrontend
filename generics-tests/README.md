# Generics regression tests

Regression tests for the Go 1.18 generics support added to this gccgo
frontend (branch `pgimalac/generics`).  Each `*.go` program exercises one
or more generic features; the committed `*.out` file is the expected
output (generated with the upstream `go` tool).

Run them against a GCC build tree that built this frontend:

    ./run.sh /path/to/gcc-build

`run.sh` compiles each program with the built `gccgo`, runs it, and
compares the output to the golden `*.out` file.  It exits non-zero on any
failure.

## Coverage

| File | Feature |
|------|---------|
| `func_explicit_min.go` | generic function, explicit type args, union constraint |
| `func_multi_param_map.go` | multiple type params, `[]T`, `func(T)U` params, closures |
| `func_compose.go` | composed generic calls, `append`/`make` |
| `infer_basic.go` | type-argument inference (direct `T`, `[]T`, `func(T)U`) |
| `infer_constraint_map.go` | inference with constraints, `comparable`, `map[K]V` |
| `grouped_type_params.go` | grouped params `[T, U any]`, multi-return, inference |
| `forward_reference.go` | generic function used before its declaration |
| `pointer_type_args.go` | explicit pointer type arg, `[]*T` inference |
| `type_pair_composite.go` | generic struct type, composite literal instantiation |
| `nested_generic_types.go` | nested generic types `Box[Pair[int,string]]` |
| `type_methods_stack.go` | methods on generic types, pointer receivers, `var zero T` |
| `recursive_type_list.go` | recursive generic type (linked list) + methods, unexported generic type |
| `constraint_typeset_sum.go` | constraint interface with type-set (`~int | ~float64`) |
| `constraint_method_call.go` | calling a constraint interface method on a type-param value |

## Known limitations (not covered, intentionally)

- Constraints are parsed but **not enforced** (invalid instantiations are
  not rejected at the constraint level).
- Generic **types** (and their methods) must be declared before first use
  (generic functions may be forward-referenced when called with inferred
  type arguments).
- Cross-package export of generic templates is not implemented.
