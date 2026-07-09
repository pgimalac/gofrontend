// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package defers

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
	"golang.org/x/tools/internal/analysis/analyzerutil"
	"golang.org/x/tools/internal/typesinternal"
)

var doc = `// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package defers defines an Analyzer that checks for common mistakes in defer
// statements.
//
// # Analyzer defers
//
// defers: report common mistakes in defer statements
//
// The defers analyzer reports a diagnostic when a defer statement would
// result in a non-deferred call to time.Since, as experience has shown
// that this is nearly always a mistake.
//
// For example:
//
//	start := time.Now()
//	...
//	defer recordLatency(time.Since(start)) // error: call to time.Since is not deferred
//
// The correct code is:
//
//	defer func() { recordLatency(time.Since(start)) }()
package defers
`

// Analyzer is the defers analyzer.
var Analyzer = &analysis.Analyzer{
	Name:     "defers",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Doc:      analyzerutil.MustExtractDoc(doc, "defers"),
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/defers",
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	if !typesinternal.Imports(pass.Pkg, "time") {
		return nil, nil
	}

	checkDeferCall := func(node ast.Node) bool {
		switch v := node.(type) {
		case *ast.CallExpr:
			if typesinternal.IsFunctionNamed(typeutil.Callee(pass.TypesInfo, v), "time", "Since") {
				pass.Reportf(v.Pos(), "call to time.Since is not deferred")
			}
		case *ast.FuncLit:
			return false // prune
		}
		return true
	}

	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.DeferStmt)(nil),
	}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		d := n.(*ast.DeferStmt)
		ast.Inspect(d.Call, checkDeferCall)
	})

	return nil, nil
}
