// Package main is the entry point for the sqlcheck binary.
//
// The package also defines the Analyzer — keeping the definition and the
// entry point together avoids an unnecessary sub-package indirection.
// Tests import this package via the _test package pattern with
// analysistest.Run.
//
// Usage:
//
//	go build -o sqlcheck ./cmd/sqlcheck
//	go vet -vettool=./sqlcheck ./...
//
// See README.md for full usage instructions and golangci-lint wiring notes.
package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer is the exported *analysis.Analyzer for use with singlechecker.Main
// or multichecker.Main.
var Analyzer = &analysis.Analyzer{
	Name:     "sqlcheck",
	Doc:      "flags SQL queries built via fmt.Sprintf or string concatenation (SQL injection risk)",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// includeTests controls whether _test.go files are checked. Defaults to false:
// test code often uses pragmatic concatenation patterns (e.g. PRAGMA queries)
// that are safe in context. Set to true via -sqlcheck.include-tests flag to
// audit tests as well.
var includeTests bool

func init() {
	Analyzer.Flags.BoolVar(&includeTests, "include-tests", false,
		"also check _test.go files (default: skip test files)")
}

// sqlMethods is the set of method names on *sql.DB, *sql.Tx, and *sql.Conn
// that accept a SQL string as their first argument (after optional ctx).
// The first positional SQL arg is always arg[0] for Query/Exec/Prepare and
// arg[1] for their Context variants (which take ctx as arg[0]).
var sqlMethods = map[string]bool{
	"Query":           true,
	"QueryRow":        true,
	"QueryContext":    true,
	"QueryRowContext": true,
	"Exec":            true,
	"ExecContext":     true,
	"Prepare":         true,
	"PrepareContext":  true,
}

// contextMethods are the *Context variants where the SQL arg is at index 1
// (the context.Context is index 0).
var contextMethods = map[string]bool{
	"QueryContext":    true,
	"QueryRowContext": true,
	"ExecContext":     true,
	"PrepareContext":  true,
}

// sqlReceiverTypes is the set of fully-qualified type paths for receiver types
// we inspect.  We match against the canonical database/sql types.
var sqlReceiverTypes = []string{
	"database/sql.DB",
	"database/sql.Tx",
	"database/sql.Conn",
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// Build a map from token.Pos line number to comment text for each file.
	// Used to detect //nolint:sqlcheck suppression on the same line as a call.
	type fileComments struct {
		fset *token.FileSet
		file *token.File
		// line -> comment text (last comment group on that line)
		lineComments map[int]string
	}
	fileCommentMap := make(map[*ast.File]*fileComments)
	for _, f := range pass.Files {
		fc := &fileComments{
			fset:         pass.Fset,
			file:         pass.Fset.File(f.Pos()),
			lineComments: make(map[int]string),
		}
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				line := fc.file.Line(c.Slash)
				fc.lineComments[line] = c.Text
			}
		}
		fileCommentMap[f] = fc
	}

	// isTestFile returns true if the given position is in a _test.go file.
	isTestFile := func(pos token.Pos) bool {
		tf := pass.Fset.File(pos)
		if tf == nil {
			return false
		}
		name := tf.Name()
		return strings.HasSuffix(name, "_test.go")
	}

	// isNolint returns true if the given position is on a line that has a
	// //nolint:sqlcheck or //nolint comment, suppressing the diagnostic.
	isNolint := func(pos token.Pos) bool {
		tf := pass.Fset.File(pos)
		if tf == nil {
			return false
		}
		line := tf.Line(pos)
		for _, f := range pass.Files {
			if fc, ok := fileCommentMap[f]; ok {
				if text, ok := fc.lineComments[line]; ok {
					lower := strings.ToLower(text)
					if strings.Contains(lower, "nolint:sqlcheck") ||
						strings.Contains(lower, "sqlcheck:ignore") {
						return true
					}
				}
			}
		}
		return false
	}

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}

		// We're looking for selector calls: receiver.MethodName(args…)
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}

		methodName := sel.Sel.Name
		if !sqlMethods[methodName] {
			return
		}

		// Verify the receiver is *sql.DB, *sql.Tx, or *sql.Conn.
		recvType := pass.TypesInfo.TypeOf(sel.X)
		if recvType == nil {
			return
		}
		if !isSQLReceiver(recvType) {
			return
		}

		// Determine the SQL string argument index:
		//   Context variants: ctx is arg[0], SQL is arg[1]
		//   Non-Context variants: SQL is arg[0]
		sqlArgIdx := 0
		if contextMethods[methodName] {
			sqlArgIdx = 1
		}

		if len(call.Args) <= sqlArgIdx {
			return
		}

		sqlArg := call.Args[sqlArgIdx]

		if reason, bad := isTainted(pass, sqlArg); bad {
			if isNolint(call.Pos()) {
				return
			}
			if !includeTests && isTestFile(call.Pos()) {
				return
			}
			pass.Reportf(call.Pos(),
				"SQL query built via %s — use ? placeholders and pass values as parameters",
				reason)
		}
	})

	return nil, nil
}

// isSQLReceiver reports whether t (possibly a pointer) is one of
// *sql.DB, *sql.Tx, or *sql.Conn.
func isSQLReceiver(t types.Type) bool {
	// Unwrap pointer.
	pt, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := pt.Elem().(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	pkg := obj.Pkg()
	if pkg == nil {
		return false
	}
	fqn := pkg.Path() + "." + obj.Name()
	for _, want := range sqlReceiverTypes {
		if fqn == want {
			return true
		}
	}
	return false
}

// isTainted inspects a SQL-string argument expression and returns
// ("reason", true) if the value is constructed unsafely, or ("", false) if it
// is provably a constant string literal or an identifier resolving to a
// constant.
//
// Detected patterns:
//
//	fmt.Sprintf(…)                  → "fmt.Sprintf"
//	fmt.Sprintf result in a local   → "fmt.Sprintf"
//	"str" + expr or expr + "str"    → "string concatenation"
//	local var := unsafe_expr        → traces one level
func isTainted(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	return isTaintedInner(pass, expr, 0)
}

const maxTraceDepth = 4 // prevent infinite loops on self-referential code

func isTaintedInner(pass *analysis.Pass, expr ast.Expr, depth int) (string, bool) {
	if depth > maxTraceDepth {
		return "", false
	}

	switch e := expr.(type) {
	case *ast.BasicLit:
		// A bare string literal like "SELECT id FROM t WHERE name=?" is always safe.
		// Non-string literals (int, float) are not SQL strings; also safe.
		_ = e
		return "", false

	case *ast.CallExpr:
		// Check for fmt.Sprintf / fmt.Fprintf / fmt.Sprintf variants.
		if isFmtSprintf(pass, e) {
			return "fmt.Sprintf", true
		}
		// Check for strings.Builder.String() — if a Builder was written to
		// with non-const data, it's tainted. But detecting that statically
		// requires tracking Builder state which is complex; we flag any
		// .String() call on a strings.Builder receiver as potentially unsafe
		// only if the Builder itself is tainted. For now we detect the
		// explicit Sprintf/concat patterns; document limitation.
		return "", false

	case *ast.BinaryExpr:
		// Flag any + on strings. Even "literal" + const is technically safe
		// (compiler folds it), but variable + anything is unsafe.
		if e.Op == token.ADD {
			// Both sides are string BasicLit or constant ident? → safe.
			leftConst := isConstantString(pass, e.X)
			rightConst := isConstantString(pass, e.Y)
			if leftConst && rightConst {
				return "", false
			}
			return "string concatenation", true
		}
		return "", false

	case *ast.Ident:
		// If it resolves to a constant (const keyword), it's safe.
		obj := pass.TypesInfo.ObjectOf(e)
		if obj == nil {
			return "", false
		}
		if _, isConst := obj.(*types.Const); isConst {
			return "", false
		}
		// For var/local, try to trace its declaration within the same file.
		if v, ok := obj.(*types.Var); ok {
			_ = v // used for type assertion only
			if initExpr := findVarInit(pass, e); initExpr != nil {
				return isTaintedInner(pass, initExpr, depth+1)
			}
		}
		return "", false

	case *ast.ParenExpr:
		return isTaintedInner(pass, e.X, depth+1)
	}

	return "", false
}

// isConstantString reports whether expr is a string constant: either a
// BasicLit string, or an identifier that resolves to a types.Const.
func isConstantString(pass *analysis.Pass, expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(e)
		if obj == nil {
			return false
		}
		_, ok := obj.(*types.Const)
		return ok
	case *ast.ParenExpr:
		return isConstantString(pass, e.X)
	}
	return false
}

// isFmtSprintf returns true if call is a call to fmt.Sprintf.
// We check the fully-qualified package path to avoid false positives from
// user-defined functions named Sprintf.
func isFmtSprintf(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Sprintf" {
		return false
	}
	// Resolve the package of the selector's X.
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	pkgName, ok := pass.TypesInfo.ObjectOf(ident).(*types.PkgName)
	if !ok {
		return false
	}
	return pkgName.Imported().Path() == "fmt"
}

// findVarInit attempts to find the initializing expression for a local
// variable referenced by ident. It searches the AST of the file containing
// the ident for an AssignStmt or ValueSpec whose LHS includes the same
// object.
//
// This is a best-effort single-scope trace: it handles the common pattern
//
//	q := fmt.Sprintf(...)
//	db.Query(q)
//
// but does NOT follow assignments across function boundaries or loops.
func findVarInit(pass *analysis.Pass, ident *ast.Ident) ast.Expr {
	targetObj := pass.TypesInfo.ObjectOf(ident)
	if targetObj == nil {
		return nil
	}

	// Find the file that contains this ident.
	pos := ident.Pos()
	var targetFile *ast.File
	for _, f := range pass.Files {
		if f.Pos() <= pos && pos <= f.End() {
			targetFile = f
			break
		}
	}
	if targetFile == nil {
		return nil
	}

	// Walk the file looking for assignments to this variable.
	var initExpr ast.Expr
	ast.Inspect(targetFile, func(n ast.Node) bool {
		if initExpr != nil {
			return false
		}
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range stmt.Lhs {
				lhsIdent, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				obj := pass.TypesInfo.ObjectOf(lhsIdent)
				if obj == targetObj && i < len(stmt.Rhs) {
					initExpr = stmt.Rhs[i]
					return false
				}
			}
		case *ast.ValueSpec:
			for i, name := range stmt.Names {
				obj := pass.TypesInfo.ObjectOf(name)
				if obj == targetObj && i < len(stmt.Values) {
					initExpr = stmt.Values[i]
					return false
				}
			}
		}
		return true
	})
	return initExpr
}
