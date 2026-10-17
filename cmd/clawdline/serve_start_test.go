package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// The "a session is waiting for you" push stopped for a month when the call
// that started its watcher was removed along with the v1 board sweep it lived
// in. Serve itself must start the watcher; the watcher's own behaviour once
// started is TestStartWaitingPushesAStandingQuestionOnce.
func TestServeStartsTheWaitingWatcher(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != "serve" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "StartWaiting" {
				called = true
			}
			return true
		})
	}
	if !called {
		t.Fatal("serve does not call StartWaiting: no session waiting on the person is ever pushed")
	}
}
