package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
// It reads the statement as the source spells it: each string literal or
// package-level string constant, joined with the others of the `+` chain it
// is part of, and each fmt.Sprintf whose format and arguments it can read.
// The SQL is then split on `;` and read with identifier quotes, a schema
// prefix, an `OR <conflict>` clause and a table alias allowed. A table name
// assembled any other way at run time, or a constant from another package,
// is outside what it can see.

// brokerTallyExceptions are the writes that do not move version, each by
// where it is (file and function) and why the snapshot cannot be holding the
// row it changes. An entry that no longer matches a statement fails too.
var brokerTallyExceptions = map[string]string{}

var (
	brokerSQLDelete  = regexp.MustCompile(`(?i)\bdelete\s+from\s+broker_tasks\b`)
	brokerSQLReplace = regexp.MustCompile(`(?i)\b(replace|or\s+replace)\s+into\s+broker_tasks\b|\bupdate\s+or\s+replace\s+broker_tasks\b`)
	brokerSQLDrop    = regexp.MustCompile(`(?i)\b(drop|truncate)\s+table\s+(if\s+exists\s+)?broker_tasks\b`)
	brokerSQLAlter   = regexp.MustCompile(`(?i)\balter\s+table\s+broker_tasks\b`)
	brokerSQLAddCol  = regexp.MustCompile(`(?i)\balter\s+table\s+broker_tasks\s+add\s+column\b`)
	brokerSQLUpdate  = regexp.MustCompile(`(?i)\bupdate\s+(?:or\s+\w+\s+)?broker_tasks(?:\s+(?:as\s+)?(\w+))?\s+set\b`)
	brokerSQLUpsert  = regexp.MustCompile(`(?i)\binsert\s+(?:or\s+\w+\s+)?into\s+broker_tasks(?:\s+as\s+(\w+))?\b[\s\S]*\bdo\s+update\s+set\b`)
	brokerSQLBump    = regexp.MustCompile(`(?i)\bversion\s*=\s*(?:(\w+)\s*\.\s*)?version\s*\+\s*1\b`)
	// brokerSQLSchema is a schema name in front of the table, as in
	// main.broker_tasks, read once identifier quotes are gone.
	brokerSQLSchema = regexp.MustCompile(`(?i)\b\w+\s*\.\s*broker_tasks\b`)
	// brokerFmtVerb is one fmt verb, or the escaped percent sign.
	brokerFmtVerb = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z%]`)
)

// brokerTallyViolation says why a statement in sql breaks the tally's
// invariant, or "".
func brokerTallyViolation(sql string) string {
	for _, stmt := range brokerSQLStatements(sql) {
		if why := brokerStatementViolation(stmt); why != "" {
			return why
		}
	}
	return ""
}

func brokerStatementViolation(stmt string) string {
	switch {
	case brokerSQLDelete.MatchString(stmt):
		return "deletes rows"
	case brokerSQLReplace.MatchString(stmt):
		return "replaces rows, which deletes the old one and restarts its version"
	case brokerSQLDrop.MatchString(stmt):
		return "drops the table"
	case brokerSQLAlter.MatchString(stmt) && !brokerSQLAddCol.MatchString(stmt):
		return "alters the table other than by adding a column"
	}
	if m := brokerSQLUpsert.FindStringSubmatch(stmt); m != nil && !brokerSQLBumped(stmt, false, "broker_tasks", m[1]) {
		return "upserts without version = broker_tasks.version + 1"
	}
	if m := brokerSQLUpdate.FindStringSubmatch(stmt); m != nil && !brokerSQLBumped(stmt, true, "broker_tasks", m[1]) {
		return "updates without version = version + 1"
	}
	return ""
}

// brokerSQLBumped says whether stmt sets version to itself plus one, the
// right-hand version read bare when bare allows it or through one of the
// qualifiers (an empty one, an absent alias, allows nothing).
func brokerSQLBumped(stmt string, bare bool, qualifiers ...string) bool {
	for _, m := range brokerSQLBump.FindAllStringSubmatch(stmt, -1) {
		if m[1] == "" && bare {
			return true
		}
		for _, q := range qualifiers {
			if q != "" && strings.EqualFold(m[1], q) {
				return true
			}
		}
	}
	return false
}

// brokerSQLStatements splits sql on each `;` outside a string literal and
// reads every statement with its string literals emptied, its identifier
// quotes (double quotes, backquotes, brackets) and any schema prefix on
// broker_tasks taken away.
func brokerSQLStatements(sql string) []string {
	var out []string
	var b strings.Builder
	for i := 0; i < len(sql); i++ {
		switch c := sql[i]; c {
		case '\'':
			j := strings.IndexByte(sql[i+1:], '\'')
			if j < 0 {
				b.WriteString(sql[i:])
				i = len(sql)
				continue
			}
			b.WriteString("''")
			i += j + 1
		case '"', '`', '[':
			end := c
			if c == '[' {
				end = ']'
			}
			j := strings.IndexByte(sql[i+1:], end)
			if j < 0 {
				b.WriteString(sql[i+1:])
				i = len(sql)
				continue
			}
			b.WriteString(sql[i+1 : i+1+j])
			i += j + 1
		case ';':
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	out = append(out, b.String())
	for i := range out {
		out[i] = brokerSQLSchema.ReplaceAllString(out[i], "broker_tasks")
	}
	return out
}

func TestBrokerTallyViolationReadsEachKindOfWrite(t *testing.T) {
	for stmt, bad := range map[string]bool{
		`DELETE FROM broker_tasks WHERE id = ?`:                                                                      true,
		`delete  from broker_tasks`:                                                                                  true,
		`INSERT OR REPLACE INTO broker_tasks (id) VALUES (?)`:                                                        true,
		`REPLACE INTO broker_tasks (id) VALUES (?)`:                                                                  true,
		`DROP TABLE IF EXISTS broker_tasks`:                                                                          true,
		`ALTER TABLE broker_tasks RENAME TO old`:                                                                     true,
		`UPDATE broker_tasks SET record = ? WHERE id = ?`:                                                            true,
		`UPDATE broker_tasks SET version = ? WHERE id = ?`:                                                           true,
		`INSERT INTO broker_tasks (id) VALUES (?) ON CONFLICT(id) DO UPDATE SET record = ?`:                          true,
		`DELETE FROM "broker_tasks"`:                                                                                 true,
		"DELETE FROM `broker_tasks`":                                                                                 true,
		`DELETE FROM [broker_tasks]`:                                                                                 true,
		`DELETE FROM "main"."broker_tasks"`:                                                                          true,
		`DELETE FROM main . broker_tasks`:                                                                            true,
		`UPDATE OR IGNORE broker_tasks SET record = ?`:                                                               true,
		`UPDATE OR REPLACE broker_tasks SET record = ?, version = version + 1`:                                       true,
		`UPDATE broker_tasks AS t SET record = ? WHERE t.id = ?`:                                                     true,
		`UPDATE broker_tasks t SET record = ?`:                                                                       true,
		`UPDATE broker_tasks AS t SET record = ?, version = other.version + 1`:                                       true,
		`SELECT 1; DELETE FROM broker_tasks`:                                                                         true,
		`UPDATE broker_tasks SET record = 'a;b', version = version + 1; UPDATE broker_tasks SET record = ?`:          true,
		`INSERT INTO broker_tasks AS t (id) VALUES (?) ON CONFLICT(id) DO UPDATE SET version = excluded.version + 1`: true,
		`UPDATE broker_tasks SET record = ?, version = version + 1 WHERE id = ?`:                                     false,
		`INSERT INTO broker_tasks (id) VALUES (?) ON CONFLICT(id) DO NOTHING`:                                        false,
		`INSERT INTO broker_tasks (id) VALUES (?) ON CONFLICT(id) DO UPDATE SET version=broker_tasks.version + 1`:    false,
		`INSERT INTO broker_tasks AS t (id) VALUES (?) ON CONFLICT(id) DO UPDATE SET version = t.version + 1`:        false,
		`ALTER TABLE broker_tasks ADD COLUMN version INTEGER NOT NULL DEFAULT 0`:                                     false,
		`SELECT id FROM broker_tasks WHERE id = ?`:                                                                   false,
		`DELETE FROM broker_task_texts WHERE task_id = ?`:                                                            false,
		`UPDATE OR IGNORE "main"."broker_tasks" AS t SET record = ?, version = t.version + 1`:                        false,
		`UPDATE broker_tasks SET record = 'DELETE FROM broker_tasks; x', version = version + 1`:                      false,
		`CREATE INDEX IF NOT EXISTS broker_tasks_updated ON broker_tasks(updated_at, version)`:                       false,
	} {
		if got := brokerTallyViolation(stmt) != ""; got != bad {
			t.Errorf("%q: violation %v, want %v", stmt, got, bad)
		}
	}
}

// brokerTallyBypasses are the spellings a review planted in the
// store package that the first guard read past. Each is a source file the
// guard must find at least one violation in.
var brokerTallyBypasses = map[string]string{
	"table name in a constant": `const probeTable = "broker_tasks"
func probeConst(db *sql.DB) { _, _ = db.Exec("DELETE FROM " + probeTable) }`,
	"table name in a Sprintf argument": `func probeSprintf(db *sql.DB) { _, _ = db.Exec(fmt.Sprintf("DELETE FROM %s", "broker_tasks")) }`,
	"quoted table name":                `func probeQuoted(db *sql.DB) { _, _ = db.Exec(` + "`" + `DELETE FROM "broker_tasks" WHERE id = ?` + "`" + `, "x") }`,
	"schema prefix":                    `func probeSchema(db *sql.DB) { _, _ = db.Exec(` + "`" + `DELETE FROM main.broker_tasks WHERE id = ?` + "`" + `, "x") }`,
	"UPDATE OR IGNORE":                 `func probeOrIgnore(db *sql.DB) { _, _ = db.Exec(` + "`" + `UPDATE OR IGNORE broker_tasks SET record = ? WHERE id = ?` + "`" + `, "{}", "x") }`,
	"UPDATE with an alias":             `func probeAlias(db *sql.DB) { _, _ = db.Exec(` + "`" + `UPDATE broker_tasks AS t SET record = ? WHERE t.id = ?` + "`" + `, "{}", "x") }`,
	"two statements in one string": `func probeTwo(db *sql.DB) {
	_, _ = db.Exec(` + "`" + `UPDATE broker_tasks SET record = '{}'; UPDATE broker_tasks SET version = version + 1 WHERE id = 'none'` + "`" + `)
}`,
	"literal in a call inside a + chain": `func probePruned(db *sql.DB) {
	_, _ = db.Exec("" + strings.TrimSpace(` + "`" + `DELETE FROM broker_tasks` + "`" + `))
}`,
	"constant spelled from constants": `const (
	probeVerb  = "DELETE FROM "
	probeQuery = probeVerb + probeTable
)
const probeTable = "broker_tasks"
func probeChained(db *sql.DB) { _, _ = db.Exec(probeQuery) }`,
	"constant in a Sprintf format": `const probeFormat = "UPDATE %s SET record = ?"
func probeFormatted(db *sql.DB) { _, _ = db.Exec(fmt.Sprintf(probeFormat, "broker_tasks")) }`,
}

// TestTheGuardSeesEverySpellingOfABadWrite drives each bypass through the
// same reading the guard gives the module's source.
func TestTheGuardSeesEverySpellingOfABadWrite(t *testing.T) {
	for name, src := range brokerTallyBypasses {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "probe.go", "package store\n"+src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			consts := brokerConstants([]*ast.File{f})
			for _, s := range brokerFileStatements(fset, f, consts) {
				if brokerTallyViolation(s.text) != "" {
					return
				}
			}
			t.Errorf("the guard reads no violation in\n%s", src)
		})
	}
}

// TestNoWriteBreaksTheBrokerTally is the guard: every statement in the
// module's non-test Go source that names broker_tasks keeps the tally sound,
// or is an exception above with its reason.
func TestNoWriteBreaksTheBrokerTally(t *testing.T) {
	root := moduleRoot(t)
	packages := map[string][]string{}
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
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			packages[filepath.Dir(path)] = append(packages[filepath.Dir(path)], path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	dirs := make([]string, 0, len(packages))
	for dir := range packages {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	seen := map[string]bool{}
	statements := 0
	for _, dir := range dirs {
		fset := token.NewFileSet()
		files := make([]*ast.File, 0, len(packages[dir]))
		for _, path := range packages[dir] {
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			files = append(files, f)
		}
		consts := brokerConstants(files)
		for i, f := range files {
			path := packages[dir][i]
			for _, s := range brokerFileStatements(fset, f, consts) {
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
		}
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

// brokerConstants is every package-level string constant of files whose
// value is a chain of literals and other such constants.
func brokerConstants(files []*ast.File) map[string]string {
	consts := map[string]string{}
	var specs []*ast.ValueSpec
	for _, f := range files {
		for _, decl := range f.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
				for _, spec := range gd.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Values) == len(vs.Names) {
						specs = append(specs, vs)
					}
				}
			}
		}
	}
	// A constant may be spelled from one declared after it, or in another
	// file, so read until a pass resolves nothing new.
	for grew := true; grew; {
		grew = false
		for _, vs := range specs {
			for i, name := range vs.Names {
				if _, ok := consts[name.Name]; ok {
					continue
				}
				if text, lit, pure := joinLiterals(vs.Values[i], consts); lit && pure {
					consts[name.Name] = text
					grew = true
				}
			}
		}
	}
	return consts
}

// brokerFileStatements is every `+` chain of string literals and constants
// in f, per function, that names broker_tasks, with the chain's other
// operands read as "?", and every fmt.Sprintf that does once its arguments
// are put in. An operand that is not a literal or constant is read for
// statements of its own.
func brokerFileStatements(fset *token.FileSet, f *ast.File, consts map[string]string) []brokerStatement {
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
				text, lit, _ := joinLiterals(e, consts)
				if lit && strings.Contains(text, "broker_tasks") {
					out = append(out, brokerStatement{fn, fset.Position(e.Pos()).Line, text})
				}
				// The chain is read whole; its literal parts are not
				// statements, but anything else in it may hold some.
				for _, op := range chainOperands(e) {
					if _, lit, pure := joinLiterals(op, consts); !lit || !pure {
						ast.Inspect(op, visit)
					}
				}
				return false
			case *ast.CallExpr:
				text, ok := sprintfText(e, consts)
				if !ok {
					return true
				}
				if strings.Contains(text, "broker_tasks") {
					out = append(out, brokerStatement{fn, fset.Position(e.Pos()).Line, text})
				}
				for _, arg := range e.Args[1:] {
					ast.Inspect(arg, visit)
				}
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

// sprintfText is what a fmt.Sprintf call with a readable format produces,
// each argument that is not a literal or constant read as "?".
func sprintfText(call *ast.CallExpr, consts map[string]string) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sprintf" || len(call.Args) == 0 {
		return "", false
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "fmt" {
		return "", false
	}
	format, lit, pure := joinLiterals(call.Args[0], consts)
	if !lit || !pure {
		return "", false
	}
	args := call.Args[1:]
	return brokerFmtVerb.ReplaceAllStringFunc(format, func(verb string) string {
		if verb == "%%" {
			return "%"
		}
		if len(args) == 0 {
			return " ? "
		}
		text, lit, pure := joinLiterals(args[0], consts)
		args = args[1:]
		if !lit || !pure {
			return " ? "
		}
		return text
	}), true
}

// chainOperands flattens a `+` chain into the operands it adds.
func chainOperands(e ast.Expr) []ast.Expr {
	switch v := e.(type) {
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return append(chainOperands(v.X), chainOperands(v.Y)...)
		}
	case *ast.ParenExpr:
		return chainOperands(v.X)
	}
	return []ast.Expr{e}
}

// joinLiterals flattens a `+` chain, reading each operand that is not a
// string literal or a constant in consts as "?"; lit says whether it held
// one at all, pure whether it held nothing else.
func joinLiterals(e ast.Expr, consts map[string]string) (text string, lit, pure bool) {
	switch v := e.(type) {
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			l, la, lp := joinLiterals(v.X, consts)
			r, ra, rp := joinLiterals(v.Y, consts)
			return l + r, la || ra, lp && rp
		}
	case *ast.ParenExpr:
		return joinLiterals(v.X, consts)
	case *ast.Ident:
		if s, ok := consts[v.Name]; ok {
			return s, true, true
		}
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			if s, err := strconv.Unquote(v.Value); err == nil {
				return s, true, true
			}
		}
	}
	return " ? ", false, false
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
