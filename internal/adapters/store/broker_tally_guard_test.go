package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The broker's ledger snapshot trusts BrokerTally (broker.go): equal tallies
// mean no write happened between them. That holds only while no row of
// broker_tasks is ever deleted or replaced, and every write either inserts a
// row or moves one row's version up by exactly one. A write that breaks it is
// not an error anywhere — the snapshot silently keeps a record the table no
// longer holds — so this guard reads every SQL statement this module ships
// that names the table, and fails on each write that does not keep it.
//
// It reads the statement as the source spells it: each string literal that
// names broker_tasks, joined with the other literals of the `+` chain it is
// part of. A table name assembled at run time is outside what it can see.

// brokerTallyExceptions are the writes that do not move version, each by
// where it is (file and function) and why the snapshot cannot be holding the
// row it changes. An entry that no longer matches a statement fails too.
var brokerTallyExceptions = map[string]string{}

var (
	brokerSQLDelete   = regexp.MustCompile(`(?i)\bdelete\s+from\s+broker_tasks\b`)
	brokerSQLReplace  = regexp.MustCompile(`(?i)\b(replace|or\s+replace)\s+into\s+broker_tasks\b`)
	brokerSQLDrop     = regexp.MustCompile(`(?i)\b(drop|truncate)\s+table\s+(if\s+exists\s+)?broker_tasks\b`)
	brokerSQLAlter    = regexp.MustCompile(`(?i)\balter\s+table\s+broker_tasks\b`)
	brokerSQLAddCol   = regexp.MustCompile(`(?i)\balter\s+table\s+broker_tasks\s+add\s+column\b`)
	brokerSQLUpdate   = regexp.MustCompile(`(?i)\bupdate\s+broker_tasks\s+set\b`)
	brokerSQLUpsert   = regexp.MustCompile(`(?i)\binsert\s+into\s+broker_tasks\b[\s\S]*\bdo\s+update\s+set\b`)
	brokerSQLBumped   = regexp.MustCompile(`(?i)\bversion\s*=\s*version\s*\+\s*1\b`)
	brokerSQLUpBumped = regexp.MustCompile(`(?i)\bversion\s*=\s*broker_tasks\.version\s*\+\s*1\b`)
)

// brokerTallyViolation says why stmt breaks the tally's invariant, or "".
func brokerTallyViolation(stmt string) string {
	switch {
	case brokerSQLDelete.MatchString(stmt):
		return "deletes rows"
	case brokerSQLReplace.MatchString(stmt):
		return "replaces rows, which deletes the old one and restarts its version"
	case brokerSQLDrop.MatchString(stmt):
		return "drops the table"
	case brokerSQLAlter.MatchString(stmt) && !brokerSQLAddCol.MatchString(stmt):
		return "alters the table other than by adding a column"
	case brokerSQLUpsert.MatchString(stmt) && !brokerSQLUpBumped.MatchString(stmt):
		return "upserts without version = broker_tasks.version + 1"
	case brokerSQLUpdate.MatchString(stmt) && !brokerSQLBumped.MatchString(stmt):
		return "updates without version = version + 1"
	}
	return ""
}

func TestBrokerTallyViolationReadsEachKindOfWrite(t *testing.T) {
	for stmt, bad := range map[string]bool{
		`DELETE FROM broker_tasks WHERE id = ?`:                                                                   true,
		`delete  from broker_tasks`:                                                                               true,
		`INSERT OR REPLACE INTO broker_tasks (id) VALUES (?)`:                                                     true,
		`REPLACE INTO broker_tasks (id) VALUES (?)`:                                                               true,
		`DROP TABLE IF EXISTS broker_tasks`:                                                                       true,
		`ALTER TABLE broker_tasks RENAME TO old`:                                                                  true,
		`UPDATE broker_tasks SET record = ? WHERE id = ?`:                                                         true,
		`UPDATE broker_tasks SET version = ? WHERE id = ?`:                                                        true,
		`INSERT INTO broker_tasks (id) VALUES (?) ON CONFLICT(id) DO UPDATE SET record = ?`:                       true,
		`UPDATE broker_tasks SET record = ?, version = version + 1 WHERE id = ?`:                                  false,
		`INSERT INTO broker_tasks (id) VALUES (?) ON CONFLICT(id) DO NOTHING`:                                     false,
		`INSERT INTO broker_tasks (id) VALUES (?) ON CONFLICT(id) DO UPDATE SET version=broker_tasks.version + 1`: false,
		`ALTER TABLE broker_tasks ADD COLUMN version INTEGER NOT NULL DEFAULT 0`:                                  false,
		`SELECT id FROM broker_tasks WHERE id = ?`:                                                                false,
		`DELETE FROM broker_task_texts WHERE task_id = ?`:                                                         false,
	} {
		if got := brokerTallyViolation(stmt) != ""; got != bad {
			t.Errorf("%q: violation %v, want %v", stmt, got, bad)
		}
	}
}

// TestNoWriteBreaksTheBrokerTally is the guard: every statement in the
// module's non-test Go source that names broker_tasks keeps the tally sound,
// or is an exception above with its reason.
func TestNoWriteBreaksTheBrokerTally(t *testing.T) {
	root := moduleRoot(t)
	seen := map[string]bool{}
	statements := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "testdata", "web":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, s := range brokerStatements(t, path) {
			statements++
			why := brokerTallyViolation(s.text)
			if why == "" {
				continue
			}
			key := filepath.Base(path) + " " + s.fn
			if _, ok := brokerTallyExceptions[key]; ok {
				seen[key] = true
				continue
			}
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s:%d (%s) %s, which breaks BrokerTally's invariant (store/broker.go): "+
				"the ledger snapshot would keep a row the table no longer holds.\n\t%s",
				rel, s.line, s.fn, why, s.text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if statements == 0 {
		t.Fatal("found no statement naming broker_tasks: the guard is reading the wrong tree")
	}
	for key := range brokerTallyExceptions {
		if !seen[key] {
			t.Errorf("exception %q matches no statement any more; remove it", key)
		}
	}
}

type brokerStatement struct {
	fn   string
	line int
	text string
}

// brokerStatements is every `+` chain of string literals in path, per
// function, that names broker_tasks, with the chain's non-literal operands
// read as "?".
func brokerStatements(t *testing.T, path string) []brokerStatement {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	out := []brokerStatement{}
	for _, decl := range f.Decls {
		fn := "(package)"
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
		}
		var visit func(n ast.Node) bool
		visit = func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.BinaryExpr:
				if e.Op != token.ADD {
					return true
				}
				text, lit := joinLiterals(e)
				if lit && strings.Contains(text, "broker_tasks") {
					out = append(out, brokerStatement{fn, fset.Position(e.Pos()).Line, text})
				}
				// The chain is read whole; its parts are not statements.
				return false
			case *ast.BasicLit:
				if e.Kind != token.STRING {
					return true
				}
				if s, err := strconv.Unquote(e.Value); err == nil && strings.Contains(s, "broker_tasks") {
					out = append(out, brokerStatement{fn, fset.Position(e.Pos()).Line, s})
				}
			}
			return true
		}
		ast.Inspect(decl, visit)
	}
	return out
}

// joinLiterals flattens a `+` chain, reading each operand that is not a
// string literal as "?"; lit says whether it held a string literal at all.
func joinLiterals(e ast.Expr) (text string, lit bool) {
	switch v := e.(type) {
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			l, la := joinLiterals(v.X)
			r, ra := joinLiterals(v.Y)
			return l + r, la || ra
		}
	case *ast.ParenExpr:
		return joinLiterals(v.X)
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			if s, err := strconv.Unquote(v.Value); err == nil {
				return s, true
			}
		}
	}
	return " ? ", false
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the store package")
		}
		dir = parent
	}
}
