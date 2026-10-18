package transcript

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestOnlyTheResolverComposesAClaudeRecordPath keeps Host.RecordPath the one
// answer to "where is this Session's conversation record". A Claude Session
// that changed directory after launch has its record under the directory it
// started in, which only the registry knows; a caller that composes the path
// from the scan's directory reads a file that does not exist, and no test of
// that caller notices because a missing record is an ordinary answer.
func TestOnlyTheResolverComposesAClaudeRecordPath(t *testing.T) {
	// Each allowed caller, by file and enclosing function.
	allowed := map[string]bool{
		"internal/adapters/transcript/host.go:ForSession":       true, // the registry row's own cwd
		"internal/adapters/transcript/host.go:claudeTranscript": true, // the resolver
		"internal/adapters/subagents/reader.go:claudeRecord":    true, // fallback when no resolver is wired
	}
	root := filepath.Join("..", "..", "..")
	var found []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					var name string
					switch f := call.Fun.(type) {
					case *ast.Ident:
						name = f.Name
					case *ast.SelectorExpr:
						name = f.Sel.Name
					}
					if name == "ClaudePath" {
						found = append(found, rel+":"+fn.Name.Name)
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, at := range found {
		if !allowed[at] {
			t.Errorf("%s composes a Claude record path itself; ask Host.RecordPath (or the Server's recordPath) instead", at)
		}
	}
	if len(found) == 0 {
		t.Fatal("found no ClaudePath call at all; the walk is not reading the source tree")
	}
}
