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

## Cross-package generics

`crosspkg/` holds cross-package tests: each `crosspkg/case_<name>/` is a
small multi-package program (a library package in `.../lib/lib.go`, an
optional deeper package in `.../base/base.go`, and a `main.go` that
imports them).  The golden `expected.out` files are produced with the
upstream `go` tool (the sources form a module, see `crosspkg/go.mod`).
Run them with:

    cd crosspkg && ./run.sh /path/to/gcc-build

Generic function and type templates are serialized into a package's export
data (a `generics` section) and re-instantiated in importing packages.
Bodies may reference the defining package's own symbols — exported or
unexported helpers (which are given external linkage), sibling generic
templates, package-level types/consts, and other imported packages (e.g.
`fmt`, `strings`), which are pulled in automatically.  Covered cases:

| Case | Feature |
|------|---------|
| `case_funcs` | generic functions across packages, inference + explicit args, constraints |
| `case_types` | generic types with methods, composite literals, multiple type params |
| `case_helpers` | generic body calling exported + unexported helpers and sibling generics |
| `case_external` | generic body using another imported package (`fmt`, `strings`) |
| `case_pkgtype` | generic type referencing a package type and const |
| `case_infer` | inferring a type parameter from a named generic-type argument |
| `case_transitive` | a generic chain across three packages (`main` → `lib` → `base`) |

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

## Additional covered features

| File | Feature |
|------|---------|
| `type_forward_reference.go` | generic type used before its declaration |
| `generic_interface.go` | generic interface type as a parameter |
| `variadic_inference.go` / `variadic_append.go` | type inference through `...T` |
| `type_switch_param.go` | type switch on a type-parameter value (via `any`) |
| `method_value.go` | method value of a generic instance |
| `interface_satisfaction.go` | generic instance assigned to a plain interface |
| `generic_func_as_value.go` | generic instance passed as a `func` value |
| `nested_inference.go` | nested generic calls with inference |
| `comparable_index.go` | `comparable` constraint with `==` |
| `underlying_named_type.go` | `~int` matched by a named type (`type MyInt int`) |
| `type_param_conversion.go` | conversion `T(x)` in a generic |
| `generic_map_type.go` | generic defined-map type with a method |
| `method_returns_generic.go` | generic method returning another instantiation |
| `closure_returns_func.go` | generic returning `func(T) T` |
| `chained_generic.go` | `Wrap(Wrap(x))` |
| `constraint_iface_methods.go` | interface constraint methods called on type-param values |
| `map_of_slices.go` | `map[int][]T` result, inference through `func(T) int` |
| `reduce_two_params.go` | two type params, accumulator pattern |
| `generic_iterator_iface.go` | generic interface dispatch (`Iter[T]`) |

## Constraint enforcement

Constraints are enforced and a violating type argument is rejected,
matching stock go:

- **type-set** constraints — inline (`int | float64`, `~int | ~string`,
  see `constraint_violation_bad.go`), exact-vs-`~` matching
  (`constraint_violation_named_bad.go`), and **named** ones including
  those that embed other type-set constraints (`constraint_named.go`,
  `constraint_named_typeset_bad.go`);
- **`comparable`** — the argument type must be comparable
  (`constraint_comparable_bad.go`);
- **method-set** (interface-method) constraints — the argument must
  implement the interface, even when the body never calls the method
  (`constraint_method_bad.go`).

A named or method constraint *imported from another package* is the one
case not checked at the constraint itself (its definition is not resolved
in the importer); such a violation is instead rejected when the instance
body is compiled.

## Notes

- **Identifier collisions** are handled: an identifier spelled like a type
  parameter works as a struct field name (`collision_field_name.go`), a
  field/method selector (`collision_selector.go`,
  `collision_method_call.go`), and a composite-literal key
  (`Pair[A, B]{A: x}` where the field is named `A`).
- **Partial type arguments** (`F[int](x)`, inferring the rest) and
  **forward references with explicit type arguments** (`F[int](x)` before
  `F` is declared) are supported (`partial_type_args.go`,
  `forward_explicit_args.go`).
- **Nested instantiation** through a type argument that is itself a
  generic instance works, including a doubly instantiated result type
  `func F[T any](v T) Box[Box[T]]` and `Wrap(Wrap(x))` (`infer_nested.go`).
