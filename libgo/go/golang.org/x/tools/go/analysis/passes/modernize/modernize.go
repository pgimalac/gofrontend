// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modernize

import (
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"go/types"
	"iter"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/edge"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/internal/analysis/analyzerutil"
	"golang.org/x/tools/internal/astutil"
	"golang.org/x/tools/internal/moreiters"
	"golang.org/x/tools/internal/packagepath"
	"golang.org/x/tools/internal/stdlib"
	"golang.org/x/tools/internal/typesinternal"
)

// gccgo: inlined doc (no //go:embed support in the libgo build).
var doc = "// Copyright 2024 The Go Authors. All rights reserved.\n// Use of this source code is governed by a BSD-style\n// license that can be found in the LICENSE file.\n\n/*\nPackage modernize provides a suite of analyzers that suggest\nsimplifications to Go code, using modern language and library\nfeatures.\n\nEach diagnostic provides a fix. Our intent is that these fixes may\nbe safely applied en masse without changing the behavior of your\nprogram. In some cases the suggested fixes are imperfect and may\nlead to (for example) unused imports or unused local variables,\ncausing build breakage. However, these problems are generally\ntrivial to fix. We regard any modernizer whose fix changes program\nbehavior to have a serious bug and will endeavor to fix it.\n\nTo apply all modernization fixes en masse, you can use the\nfollowing command:\n\n\t$ go run golang.org/x/tools/go/analysis/passes/modernize/cmd/modernize@latest -fix ./...\n\n(Do not use \"go get -tool\" to add gopls as a dependency of your\nmodule; gopls commands must be built from their release branch.)\n\nIf the tool warns of conflicting fixes, you may need to run it more\nthan once until it has applied all fixes cleanly. This command is\nnot an officially supported interface and may change in the future.\n\nChanges produced by this tool should be reviewed as usual before\nbeing merged. In some cases, a loop may be replaced by a simple\nfunction call, causing comments within the loop to be discarded.\nHuman judgment may be required to avoid losing comments of value.\n\nThe modernize suite contains many analyzers. Diagnostics from some,\nsuch as \"any\" (which replaces \"interface{}\" with \"any\" where it\nis safe to do so), are particularly numerous. It may ease the burden of\ncode review to apply fixes in two steps, the first consisting only of\nfixes from the \"any\" analyzer, the second consisting of all\nother analyzers. This can be achieved using flags, as in this example:\n\n\t$ modernize -any=true  -fix ./...\n\t$ modernize -any=false -fix ./...\n\n# Analyzer appendclipped\n\nappendclipped: simplify append chains using slices.Concat\n\nThe appendclipped analyzer suggests replacing chains of append calls with a\nsingle call to slices.Concat, which was added in Go 1.21. For example,\nappend(append(s, s1...), s2...) would be simplified to slices.Concat(s, s1, s2).\n\nIn the simple case of appending to a newly allocated slice, such as\nappend([]T(nil), s...), the analyzer suggests the more concise slices.Clone(s).\nFor byte slices, it will prefer bytes.Clone if the \"bytes\" package is\nalready imported.\n\nThis fix is only applied when the base of the append tower is a\n\"clipped\" slice, meaning its length and capacity are equal (e.g.\nx[:0:0] or []T{}). This is to avoid changing program behavior by\neliminating intended side effects on the base slice's underlying\narray.\n\nThis analyzer is currently disabled by default as the\ntransformation does not preserve the nilness of the base slice in\nall cases; see https://go.dev/issue/73557.\n\n# Analyzer bloop\n\nbloop: replace for-range over b.N with b.Loop\n\nThe bloop analyzer suggests replacing benchmark loops of the form\n`for i := 0; i < b.N; i++` or `for range b.N` with the more modern\n`for b.Loop()`, which was added in Go 1.24.\n\nThis change makes benchmark code more readable and also removes the need for\nmanual timer control, so any preceding calls to b.StartTimer, b.StopTimer,\nor b.ResetTimer within the same function will also be removed.\n\nCaveats: The b.Loop() method is designed to prevent the compiler from\noptimizing away the benchmark loop, which can occasionally result in\nslower execution due to increased allocations in some specific cases.\nSince its fix may change the performance of nanosecond-scale benchmarks,\nbloop is disabled by default in the `go fix` analyzer suite; see golang/go#74967.\n\n# Analyzer any\n\nany: replace interface{} with any\n\nThe any analyzer suggests replacing uses of the empty interface type,\n`interface{}`, with the `any` alias, which was introduced in Go 1.18.\nThis is a purely stylistic change that makes code more readable.\n\n# Analyzer errorsastype\n\nerrorsastype: replace errors.As with errors.AsType[T]\n\nThis analyzer suggests fixes to simplify uses of [errors.As] of\nthis form:\n\n\tvar myerr *MyErr\n\tif errors.As(err, &myerr) {\n\t\thandle(myerr)\n\t}\n\nby using the less error-prone generic [errors.AsType] function,\nintroduced in Go 1.26:\n\n\tif myerr, ok := errors.AsType[*MyErr](err); ok {\n\t\thandle(myerr)\n\t}\n\nThe fix is only offered if the var declaration has the form shown and\nthere are no uses of myerr outside the if statement.\n\n# Analyzer fmtappendf\n\nfmtappendf: replace []byte(fmt.Sprintf) with fmt.Appendf\n\nThe fmtappendf analyzer suggests replacing `[]byte(fmt.Sprintf(...))` with\n`fmt.Appendf(nil, ...)`. This avoids the intermediate allocation of a string\nby Sprintf, making the code more efficient. The suggestion also applies to\nfmt.Sprint and fmt.Sprintln.\n\n# Analyzer forvar\n\nforvar: remove redundant re-declaration of loop variables\n\nThe forvar analyzer removes unnecessary shadowing of loop variables.\nBefore Go 1.22, it was common to write `for _, x := range s { x := x ... }`\nto create a fresh variable for each iteration. Go 1.22 changed the semantics\nof `for` loops, making this pattern redundant. This analyzer removes the\nunnecessary `x := x` statement.\n\nThis fix only applies to `range` loops.\n\n# Analyzer mapsloop\n\nmapsloop: replace explicit loops over maps with calls to maps package\n\nThe mapsloop analyzer replaces loops of the form\n\n\tfor k, v := range x { m[k] = v }\n\nwith a single call to a function from the `maps` package, added in Go 1.23.\nDepending on the context, this could be `maps.Copy`, `maps.Insert`,\n`maps.Clone`, or `maps.Collect`.\n\nThe transformation to `maps.Clone` is applied conservatively, as it\npreserves the nilness of the source map, which may be a subtle change in\nbehavior if the original code did not handle a nil map in the same way.\n\n# Analyzer minmax\n\nminmax: replace if/else statements with calls to min or max\n\nThe minmax analyzer simplifies conditional assignments by suggesting the use\nof the built-in `min` and `max` functions, introduced in Go 1.21. For example,\n\n\tif a < b { x = a } else { x = b }\n\nis replaced by\n\n\tx = min(a, b).\n\nThis analyzer avoids making suggestions for floating-point types,\nas the behavior of `min` and `max` with NaN values can differ from\nthe original if/else statement.\n\n# Analyzer newexpr\n\nnewexpr: simplify code by using go1.26's new(expr)\n\nThis analyzer finds declarations of functions of this form:\n\n\tfunc varOf(x int) *int { return &x }\n\nand suggests a fix to turn them into inlinable wrappers around\ngo1.26's built-in new(expr) function:\n\n\t//go:fix inline\n\tfunc varOf(x int) *int { return new(x) }\n\n(The directive comment causes the 'inline' analyzer to suggest\nthat calls to such functions are inlined.)\n\nIn addition, this analyzer suggests a fix for each call\nto one of the functions before it is transformed, so that\n\n\tuse(varOf(123))\n\nis replaced by:\n\n\tuse(new(123))\n\nWrapper functions such as varOf are common when working with Go\nserialization packages such as for JSON or protobuf, where pointers\nare often used to express optionality.\n\n# Analyzer omitzero\n\nomitzero: suggest replacing omitempty with omitzero for struct fields\n\nThe omitzero analyzer identifies uses of the `omitempty` JSON struct\ntag on fields that are themselves structs. For struct-typed fields,\nthe `omitempty` tag has no effect on the behavior of json.Marshal and\njson.Unmarshal. The analyzer offers two suggestions: either remove the\ntag, or replace it with `omitzero` (added in Go 1.24), which correctly\nomits the field if the struct value is zero.\n\nHowever, some other serialization packages (notably kubebuilder, see\nhttps://book.kubebuilder.io/reference/markers.html) may have their own\ninterpretation of the `json:\",omitzero\"` tag, so removing it may affect\nprogram behavior. For this reason, the omitzero modernizer will not\nmake changes in any package that contains +kubebuilder annotations.\n\nReplacing `omitempty` with `omitzero` is a change in behavior. The\noriginal code would always encode the struct field, whereas the\nmodified code will omit it if it is a zero-value.\n\n# Analyzer plusbuild\n\nplusbuild: remove obsolete //+build comments\n\nThe plusbuild analyzer suggests a fix to remove obsolete build tags\nof the form:\n\n\t//+build linux,amd64\n\nin files that also contain a Go 1.18-style tag such as:\n\n\t//go:build linux && amd64\n\n(It does not check that the old and new tags are consistent;\nthat is the job of the 'buildtag' analyzer in the vet suite.)\n\n# Analyzer rangeint\n\nrangeint: replace 3-clause for loops with for-range over integers\n\nThe rangeint analyzer suggests replacing traditional for loops such\nas\n\n\tfor i := 0; i < n; i++ { ... }\n\nwith the more idiomatic Go 1.22 style:\n\n\tfor i := range n { ... }\n\nThis transformation is applied only if (a) the loop variable is not\nmodified within the loop body and (b) the loop's limit expression\nis not modified within the loop, as `for range` evaluates its\noperand only once.\n\n# Analyzer reflecttypefor\n\nreflecttypefor: replace reflect.TypeOf(x) with TypeFor[T]()\n\nThis analyzer suggests fixes to replace uses of reflect.TypeOf(x) with\nreflect.TypeFor, introduced in go1.22, when the desired runtime type\nis known at compile time, for example:\n\n\treflect.TypeOf(uint32(0))        -> reflect.TypeFor[uint32]()\n\treflect.TypeOf((*ast.File)(nil)) -> reflect.TypeFor[*ast.File]()\n\nIt also offers a fix to simplify the construction below, which uses\nreflect.TypeOf to return the runtime type for an interface type,\n\n\treflect.TypeOf((*io.Reader)(nil)).Elem()\n\nto:\n\n\treflect.TypeFor[io.Reader]()\n\nNo fix is offered in cases when the runtime type is dynamic, such as:\n\n\tvar r io.Reader = ...\n\treflect.TypeOf(r)\n\nor when the operand has potential side effects.\n\n# Analyzer slicescontains\n\nslicescontains: replace loops with slices.Contains or slices.ContainsFunc\n\nThe slicescontains analyzer simplifies loops that check for the existence of\nan element in a slice. It replaces them with calls to `slices.Contains` or\n`slices.ContainsFunc`, which were added in Go 1.21.\n\nIf the expression for the target element has side effects, this\ntransformation will cause those effects to occur only once, not\nonce per tested slice element.\n\n# Analyzer slicesdelete\n\nslicesdelete: replace append-based slice deletion with slices.Delete\n\nThe slicesdelete analyzer suggests replacing the idiom\n\n\ts = append(s[:i], s[j:]...)\n\nwith the more explicit\n\n\ts = slices.Delete(s, i, j)\n\nintroduced in Go 1.21.\n\nThis analyzer is disabled by default. The `slices.Delete` function\nzeros the elements between the new length and the old length of the\nslice to prevent memory leaks, which is a subtle difference in\nbehavior compared to the append-based idiom; see https://go.dev/issue/73686.\n\n# Analyzer slicessort\n\nslicessort: replace sort.Slice with slices.Sort for basic types\n\nThe slicessort analyzer simplifies sorting slices of basic ordered\ntypes. It replaces\n\n\tsort.Slice(s, func(i, j int) bool { return s[i] < s[j] })\n\nwith the simpler `slices.Sort(s)`, which was added in Go 1.21.\n\n# Analyzer stditerators\n\nstditerators: use iterators instead of Len/At-style APIs\n\nThis analyzer suggests a fix to replace each loop of the form:\n\n\tfor i := 0; i < x.Len(); i++ {\n\t\tuse(x.At(i))\n\t}\n\nor its \"for elem := range x.Len()\" equivalent by a range loop over an\niterator offered by the same data type:\n\n\tfor elem := range x.All() {\n\t\tuse(x.At(i)\n\t}\n\nwhere x is one of various well-known types in the standard library.\n\n# Analyzer stringscut\n\nstringscut: replace strings.Index etc. with strings.Cut\n\nThis analyzer replaces certain patterns of use of [strings.Index] and string slicing by [strings.Cut], added in go1.18.\n\nFor example:\n\n\tidx := strings.Index(s, substr)\n\tif idx >= 0 {\n\t    return s[:idx]\n\t}\n\nis replaced by:\n\n\tbefore, _, ok := strings.Cut(s, substr)\n\tif ok {\n\t    return before\n\t}\n\nAnd:\n\n\tidx := strings.Index(s, substr)\n\tif idx >= 0 {\n\t    return\n\t}\n\nis replaced by:\n\n\tfound := strings.Contains(s, substr)\n\tif found {\n\t    return\n\t}\n\nIt also handles variants using [strings.IndexByte] instead of Index, or the bytes package instead of strings.\n\nFixes are offered only in cases in which there are no potential modifications of the idx, s, or substr expressions between their definition and use.\n\n# Analyzer stringscutprefix\n\nstringscutprefix: replace HasPrefix/TrimPrefix with CutPrefix\n\nThe stringscutprefix analyzer simplifies a common pattern where code first\nchecks for a prefix with `strings.HasPrefix` and then removes it with\n`strings.TrimPrefix`. It replaces this two-step process with a single call\nto `strings.CutPrefix`, introduced in Go 1.20. The analyzer also handles\nthe equivalent functions in the `bytes` package.\n\nFor example, this input:\n\n\tif strings.HasPrefix(s, prefix) {\n\t    use(strings.TrimPrefix(s, prefix))\n\t}\n\nis fixed to:\n\n\tif after, ok := strings.CutPrefix(s, prefix); ok {\n\t    use(after)\n\t}\n\nThe analyzer also offers fixes to use CutSuffix in a similar way.\nThis input:\n\n\tif strings.HasSuffix(s, suffix) {\n\t    use(strings.TrimSuffix(s, suffix))\n\t}\n\nis fixed to:\n\n\tif before, ok := strings.CutSuffix(s, suffix); ok {\n\t    use(before)\n\t}\n\n# Analyzer stringsseq\n\nstringsseq: replace ranging over Split/Fields with SplitSeq/FieldsSeq\n\nThe stringsseq analyzer improves the efficiency of iterating over substrings.\nIt replaces\n\n\tfor range strings.Split(...)\n\nwith the more efficient\n\n\tfor range strings.SplitSeq(...)\n\nwhich was added in Go 1.24 and avoids allocating a slice for the\nsubstrings. The analyzer also handles strings.Fields and the\nequivalent functions in the bytes package.\n\n# Analyzer stringsbuilder\n\nstringsbuilder: replace += with strings.Builder\n\nThis analyzer replaces repeated string += string concatenation\noperations with calls to Go 1.10's strings.Builder.\n\nFor example:\n\n\tvar s = \"[\"\n\tfor x := range seq {\n\t\ts += x\n\t\ts += \".\"\n\t}\n\ts += \"]\"\n\tuse(s)\n\nis replaced by:\n\n\tvar s strings.Builder\n\ts.WriteString(\"[\")\n\tfor x := range seq {\n\t\ts.WriteString(x)\n\t\ts.WriteString(\".\")\n\t}\n\ts.WriteString(\"]\")\n\tuse(s.String())\n\nThis avoids quadratic memory allocation and improves performance.\n\nThe analyzer requires that all references to s except the final one\nare += operations. To avoid warning about trivial cases, at least one\nmust appear within a loop. The variable s must be a local\nvariable, not a global or parameter.\n\nThe sole use of the finished string must be the last reference to the\nvariable s. (It may appear within an intervening loop or function literal,\nsince even s.String() is called repeatedly, it does not allocate memory.)\n\n# Analyzer testingcontext\n\ntestingcontext: replace context.WithCancel with t.Context in tests\n\nThe testingcontext analyzer simplifies context management in tests. It\nreplaces the manual creation of a cancellable context,\n\n\tctx, cancel := context.WithCancel(context.Background())\n\tdefer cancel()\n\nwith a single call to t.Context(), which was added in Go 1.24.\n\nThis change is only suggested if the `cancel` function is not used\nfor any other purpose.\n\n# Analyzer waitgroup\n\nwaitgroup: replace wg.Add(1)/go/wg.Done() with wg.Go\n\nThe waitgroup analyzer simplifies goroutine management with `sync.WaitGroup`.\nIt replaces the common pattern\n\n\twg.Add(1)\n\tgo func() {\n\t\tdefer wg.Done()\n\t\t...\n\t}()\n\nwith a single call to\n\n\twg.Go(func(){ ... })\n\nwhich was added in Go 1.25.\n*/\npackage modernize\n"

// Suite lists all modernize analyzers.
var Suite = []*analysis.Analyzer{
	AnyAnalyzer,
	// AppendClippedAnalyzer, // not nil-preserving!
	// BLoopAnalyzer, // may skew benchmark results, see golang/go#74967
	FmtAppendfAnalyzer,
	ForVarAnalyzer,
	MapsLoopAnalyzer,
	MinMaxAnalyzer,
	NewExprAnalyzer,
	OmitZeroAnalyzer,
	plusBuildAnalyzer,
	RangeIntAnalyzer,
	ReflectTypeForAnalyzer,
	SlicesContainsAnalyzer,
	// SlicesDeleteAnalyzer, // not nil-preserving!
	SlicesSortAnalyzer,
	stditeratorsAnalyzer,
	stringscutAnalyzer,
	StringsCutPrefixAnalyzer,
	StringsSeqAnalyzer,
	StringsBuilderAnalyzer,
	TestingContextAnalyzer,
	WaitGroupAnalyzer,
}

// -- helpers --

// formatExprs formats a comma-separated list of expressions.
func formatExprs(fset *token.FileSet, exprs []ast.Expr) string {
	var buf strings.Builder
	for i, e := range exprs {
		if i > 0 {
			buf.WriteString(",  ")
		}
		format.Node(&buf, fset, e) // ignore errors
	}
	return buf.String()
}

// isZeroIntConst reports whether e is an integer whose value is 0.
func isZeroIntConst(info *types.Info, e ast.Expr) bool {
	return isIntLiteral(info, e, 0)
}

// isIntLiteral reports whether e is an integer with given value.
func isIntLiteral(info *types.Info, e ast.Expr, n int64) bool {
	return info.Types[e].Value == constant.MakeInt64(n)
}

// filesUsingGoVersion returns a cursor for each *ast.File in the inspector
// that uses at least the specified version of Go (e.g. "go1.24").
//
// The pass's analyzer must require [inspect.Analyzer].
//
// TODO(adonovan): opt: eliminate this function, instead following the
// approach of [fmtappendf], which uses typeindex and
// [analyzerutil.FileUsesGoVersion]; see "Tip" documented at the
// latter function for motivation.
func filesUsingGoVersion(pass *analysis.Pass, version string) iter.Seq[inspector.Cursor] {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	return func(yield func(inspector.Cursor) bool) {
		for curFile := range inspect.Root().Children() {
			file := curFile.Node().(*ast.File)
			if analyzerutil.FileUsesGoVersion(pass, file, version) && !yield(curFile) {
				break
			}
		}
	}
}

// within reports whether the current pass is analyzing one of the
// specified standard packages or their dependencies.
func within(pass *analysis.Pass, pkgs ...string) bool {
	path := pass.Pkg.Path()
	return packagepath.IsStdPackage(path) &&
		moreiters.Contains(stdlib.Dependencies(pkgs...), path)
}

// unparenEnclosing removes enclosing parens from cur in
// preparation for a call to [Cursor.ParentEdge].
func unparenEnclosing(cur inspector.Cursor) inspector.Cursor {
	for astutil.IsChildOf(cur, edge.ParenExpr_X) {
		cur = cur.Parent()
	}
	return cur
}

var (
	builtinAny     = types.Universe.Lookup("any")
	builtinAppend  = types.Universe.Lookup("append")
	builtinBool    = types.Universe.Lookup("bool")
	builtinInt     = types.Universe.Lookup("int")
	builtinFalse   = types.Universe.Lookup("false")
	builtinLen     = types.Universe.Lookup("len")
	builtinMake    = types.Universe.Lookup("make")
	builtinNew     = types.Universe.Lookup("new")
	builtinNil     = types.Universe.Lookup("nil")
	builtinString  = types.Universe.Lookup("string")
	builtinTrue    = types.Universe.Lookup("true")
	byteSliceType  = types.NewSlice(types.Typ[types.Byte])
	omitemptyRegex = regexp.MustCompile(`(?:^json| json):"[^"]*(,omitempty)(?:"|,[^"]*")\s?`)
)

// lookup returns the symbol denoted by name at the position of the cursor.
func lookup(info *types.Info, cur inspector.Cursor, name string) types.Object {
	scope := typesinternal.EnclosingScope(info, cur)
	_, obj := scope.LookupParent(name, cur.Node().Pos())
	return obj
}
