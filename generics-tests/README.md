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

## Known limitations

- **Constraint enforcement** is partial: inline type-sets of predeclared
  basic types (e.g. `int | float64`, `~int | ~string`) **are** enforced
  (see `constraint_violation_bad.go`), matching stock go.  Named type-set
  constraints (e.g. a `Number` interface), `comparable`, and method
  interfaces are parsed but not enforced.
- **Identifier collisions**: because instantiation is done by textual
  substitution of the type-parameter names, a field/variable/parameter
  name that is *identical* to a type-parameter name (e.g.
  `type Pair[A, B any] struct{ A A }`) is mis-substituted.  Use distinct
  names (the conventional `First`/`Second`) — see `method_returns_generic.go`.
- **Partial type arguments**: `F[int](x)` that leaves remaining type
  parameters to be inferred is not supported; give all or none.
- **Cross-package** export/import of generic templates is not implemented.
