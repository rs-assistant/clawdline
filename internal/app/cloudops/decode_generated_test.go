package cloudops

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
)

// The generated decoders (zz_decode_generated.go, from the vocabulary table)
// against the hand-written decoders and routes they will replace.
//
// Each row is driven with every body the table lists, every body one field
// away from one (a field dropped, or one added — every field the table names
// and every key any hand-written decoder's key-set check spells, so a decoder
// that starts taking a brand-new field is caught by the name it took), and
// every listed body with one value swapped for a probe: wrong types, empty
// and odd strings, and values at and one past each limit the table states.
// For each, both decoders must agree: both refuse, or both accept with the
// same answer identity and the same local request, byte for byte.
func TestGeneratedDecodersDecideAsTheHandWrittenOnes(t *testing.T) {
	ops, err := cloudvocab.Operations()
	if err != nil {
		t.Fatal(err)
	}
	universe := bodyKeyUniverse(t, ops)
	_, compared := handWrittenLiterals(t)
	examples := map[string]any{}
	for _, row := range ops {
		for key, value := range row.Example {
			if _, ok := examples[key]; !ok {
				examples[key] = value
			}
		}
	}
	generated, hand := 0, 0
	for _, row := range ops {
		decode, isGenerated := generatedDecoders[row.Op]
		_, isHand := handDecoded[row.Op]
		if isGenerated == isHand {
			t.Errorf("%s: generated=%v and hand=%v; a row is exactly one", row.Op, isGenerated, isHand)
			continue
		}
		if isHand {
			hand++
			continue
		}
		generated++
		o, ok := catalog[row.Op]
		if !ok {
			t.Errorf("%s: generated, and this machine registers no such word", row.Op)
			continue
		}
		t.Run(row.Op, func(t *testing.T) {
			failures := 0
			for _, b := range decoderProbes(row, universe, examples, compared) {
				if msg := compareDecoders(o, decode, b); msg != "" {
					failures++
					if failures <= 6 {
						t.Errorf("body %s: %s", describeBody(b), msg)
					}
				}
			}
			if failures > 6 {
				t.Errorf("…and %d more bodies the two decoders disagree on", failures-6)
			}
		})
	}
	if len(generatedDecoders) != generated || len(handDecoded) != hand {
		t.Errorf("the generated file names %d generated and %d hand words; the table %d and %d",
			len(generatedDecoders), len(handDecoded), generated, hand)
	}
	t.Logf("%d words generated, %d decoded by hand", generated, hand)
}

// compareDecoders is "" when both decoders decide b alike, or how they differ.
func compareDecoders(o op, decode genDecoder, b body) string {
	oldPlan, oldOK := o.decode(cloneBody(b))
	newPlan, newReq, newOK := decode(cloneBody(b))
	if oldOK != newOK {
		return fmt.Sprintf("hand-written accepts=%v, generated accepts=%v", oldOK, newOK)
	}
	if !oldOK {
		return ""
	}
	if got, want := planIdentity(newPlan), planIdentity(oldPlan); got != want {
		return fmt.Sprintf("identity: generated %s, hand-written %s", got, want)
	}
	var oldReq LocalRequest
	if o.route != nil {
		oldReq = o.route(oldPlan)
	}
	if got, want := describeRequest(newReq), describeRequest(oldReq); got != want {
		return fmt.Sprintf("local request:\n  generated    %s\n  hand-written %s", got, want)
	}
	return ""
}

func planIdentity(p plan) string {
	return fmt.Sprintf("session=%q name=%q request=%q target=%q generation=%q",
		p.session, p.name, p.request, p.target, p.executionGeneration)
}

// describeRequest spells a local request so that two compare equal exactly
// when a router would see the same request: an empty query or header map is
// none, and a nil body is no body.
func describeRequest(r LocalRequest) string {
	pairs := func(m map[string]string) string {
		keys := make([]string, 0, len(m))
		for key := range m {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := []string{}
		for _, key := range keys {
			out = append(out, strconv.Quote(key)+"="+strconv.Quote(m[key]))
		}
		return strings.Join(out, "&")
	}
	body := "none"
	if r.Body != nil {
		body = strconv.Quote(string(r.Body))
	}
	return fmt.Sprintf("%s %s query[%s] header[%s] body %s", r.Method, r.Path, pairs(r.Query), pairs(r.Header), body)
}

func describeBody(b body) string {
	out, err := json.Marshal(b)
	if err != nil {
		return fmt.Sprint(map[string]any(b))
	}
	if len(out) > 240 {
		return string(out[:240]) + "…"
	}
	return string(out)
}

func cloneBody(b body) body {
	out := make(body, len(b))
	for key, value := range b {
		out[key] = cloneValue(value)
	}
	return out
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for key, value := range t {
			out[key] = cloneValue(value)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, value := range t {
			out[i] = cloneValue(value)
		}
		return out
	}
	return v
}

// bodyKeyUniverse is every key a probe may add: each field the table names,
// each key any hand-written key-set check (`has`, `hasOneOf`) spells, and one
// name nothing anywhere spells.
func bodyKeyUniverse(t *testing.T, ops []cloudvocab.Operation) []string {
	t.Helper()
	keys := map[string]bool{"brand_new_field": true}
	for _, row := range ops {
		for _, shape := range row.Shapes {
			for _, field := range shape.Body {
				keys[field] = true
			}
		}
	}
	for key := range handWrittenBodyKeys(t) {
		keys[key] = true
	}
	delete(keys, "type")
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// handWrittenBodyKeys is every string literal inside a `has` or `hasOneOf`
// call in this package's own source.
func handWrittenBodyKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys, _ := handWrittenLiterals(t)
	return keys
}

// handWrittenLiterals reads this package's own source once: every string
// literal inside a `has` or `hasOneOf` call (a body key), and every one a
// value is compared with by `==`, `!=` or a `case` (an allowed value), so a
// value a hand-written decoder still allows and the table dropped is probed.
func handWrittenLiterals(t *testing.T) (keys, compared map[string]bool) {
	t.Helper()
	literalsOnce.Do(func() { literalKeys, literalCompared, literalErr = scanHandWrittenLiterals() })
	if literalErr != nil {
		t.Fatal(literalErr)
	}
	if len(literalKeys) < 50 || len(literalCompared) < 20 {
		t.Fatalf("found %d keys and %d compared values; the source scan is not reading this package",
			len(literalKeys), len(literalCompared))
	}
	return literalKeys, literalCompared
}

var (
	literalsOnce                  sync.Once
	literalKeys, literalCompared map[string]bool
	literalErr                    error
)

func scanHandWrittenLiterals() (map[string]bool, map[string]bool, error) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		return nil, nil, err
	}
	keys, compared := map[string]bool{}, map[string]bool{}
	collect := func(into map[string]bool, n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if value, err := strconv.Unquote(lit.Value); err == nil {
					into[value] = true
				}
			}
			return true
		})
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "zz_") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			return nil, nil, err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if selector, ok := n.Fun.(*ast.SelectorExpr); ok &&
					(selector.Sel.Name == "has" || selector.Sel.Name == "hasOneOf") {
					for _, arg := range n.Args {
						collect(keys, arg)
					}
				}
			case *ast.BinaryExpr:
				if n.Op == token.EQL || n.Op == token.NEQ {
					for _, side := range []ast.Expr{n.X, n.Y} {
						if lit, ok := side.(*ast.BasicLit); ok {
							collect(compared, lit)
						}
					}
				}
			case *ast.CaseClause:
				for _, value := range n.List {
					if lit, ok := value.(*ast.BasicLit); ok {
						collect(compared, lit)
					}
				}
			}
			return true
		})
	}
	return keys, compared, nil
}

// decoderProbes is every body one row is tried with.
func decoderProbes(row cloudvocab.Operation, universe []string, examples map[string]any, compared map[string]bool) []body {
	out := []body{}
	for _, shape := range row.Shapes {
		example := row.Example
		if shape.Example != nil {
			example = shape.Example
		}
		base := body{"type": row.Op}
		for _, field := range shape.Body {
			base[field] = example[field]
		}
		out = append(out, base)
		// One field dropped, and one added.
		for _, field := range shape.Body {
			dropped := cloneBody(base)
			delete(dropped, field)
			out = append(out, dropped)
		}
		for _, field := range universe {
			if _, present := base[field]; present {
				continue
			}
			added := cloneBody(base)
			if value, ok := row.Example[field]; ok {
				added[field] = value
			} else if value, ok := examples[field]; ok {
				added[field] = value
			} else {
				added[field] = "x"
			}
			out = append(out, added)
		}
		// One value swapped for each probe.
		for _, field := range append([]string{"type"}, shape.Body...) {
			values := genericProbes()
			if rule, ok := row.Decode.Fields[field]; ok {
				values = append(values, ruleProbes(rule, base[field])...)
			}
			if _, isString := base[field].(string); isString {
				for value := range compared {
					values = append(values, value)
				}
			}
			for _, value := range values {
				swapped := cloneBody(base)
				swapped[field] = value
				out = append(out, swapped)
			}
		}
	}
	return out
}

// genericProbes is one of each kind of value, and the strings an identity
// field or a check is likeliest to get wrong.
func genericProbes() []any {
	return []any{
		nil, true, false,
		json.Number("0"), json.Number("1"), json.Number("-1"), json.Number("1.5"), json.Number("1e3"),
		json.Number("16000"), json.Number("1024"), json.Number("99999999999999999999"),
		"", "x", "A", " ", "\t", "\x01", "\x7f", "%19", "a/b", "a b", "/abs/path", "relative/path", "../x", "x/../y",
		".hidden", "a.md", "dir/a.md", "a.go", "é", "true", "false", "thumb", "summary", "full", "list",
		MachineReplySession, "0123456789abcdef0123456789abcdef", "0123456789ABCDEF0123456789ABCDEF",
		"0123456789abcdef0123456789abcde", "8eca80fffc9359d5f0fca31f3e36b741bd50218b658fc086f05931747bd4c5ce",
		"00000000-0000-4000-8000-000000000000", "wt-0123456789abcdefghijklmn", "1d", "12h", "1700000000",
		strings.Repeat("a", 129), strings.Repeat("a", 257),
		map[string]any{}, map[string]any{"k": "v"}, []any{}, []any{"a"}, []any{""}, []any{json.Number("1")},
		[]any{"a", "b"}, []any{nil},
	}
}

var hexLetter = regexp.MustCompile(`^[0-9a-f]*$`)

// ruleProbes is the values at and one past each limit the rule states, and
// each nested property swapped, dropped and joined by a stranger.
func ruleProbes(rule cloudvocab.Field, valid any) []any {
	out := []any{}
	fill := "a"
	if s, ok := valid.(string); ok && s != "" {
		fill = s[len(s)-1:]
		if !hexLetter.MatchString(fill) && strings.Contains(strings.Join(rule.Checks, ","), "hex") {
			fill = "a"
		}
	}
	str := func(n int) string {
		if n < len(rule.Prefix) {
			return rule.Prefix[:n]
		}
		return rule.Prefix + strings.Repeat(fill, n-len(rule.Prefix))
	}
	switch rule.Type {
	case cloudvocab.TypeString:
		for _, n := range []int{rule.MaxBytes, rule.Length} {
			if n > 0 {
				out = append(out, str(n-1), str(n), str(n+1))
			}
		}
		for _, value := range rule.Enum {
			out = append(out, value, value+"x", strings.ToUpper(value))
		}
		if s, ok := valid.(string); ok {
			out = append(out, s+"x", "x"+s, strings.ToUpper(s))
		}
	case cloudvocab.TypeInteger:
		for _, bound := range []*int64{rule.Min, rule.Max} {
			if bound != nil {
				for _, n := range []int64{*bound - 1, *bound, *bound + 1} {
					out = append(out, json.Number(strconv.FormatInt(n, 10)))
				}
			}
		}
	case cloudvocab.TypeObject:
		if rule.MaxBytes > 0 {
			// {"k":"…"} encodes to eight bytes more than its string.
			for _, n := range []int{rule.MaxBytes - 1, rule.MaxBytes, rule.MaxBytes + 1} {
				out = append(out, map[string]any{"k": strings.Repeat("a", n-8)})
			}
		}
		object, _ := valid.(map[string]any)
		if object == nil {
			break
		}
		for key := range object {
			dropped := cloneValue(object).(map[string]any)
			delete(dropped, key)
			out = append(out, dropped)
			for _, value := range genericProbes() {
				swapped := cloneValue(object).(map[string]any)
				swapped[key] = value
				out = append(out, swapped)
			}
		}
		joined := cloneValue(object).(map[string]any)
		joined["brand_new_field"] = "x"
		out = append(out, joined)
		for key := range rule.Properties {
			if _, present := object[key]; !present {
				added := cloneValue(object).(map[string]any)
				added[key] = nil
				out = append(out, added)
			}
		}
		for n := 0; n <= rule.MaxKeys+1 && rule.MaxKeys > 0; n++ {
			grown := map[string]any{}
			for key := range rule.Properties {
				if len(grown) < n {
					grown[key] = nil
				}
			}
			for i := len(grown); i < n; i++ {
				grown[fmt.Sprintf("k%d", i)] = "v"
			}
			out = append(out, grown)
		}
	case cloudvocab.TypeList:
		list, _ := valid.([]any)
		for _, n := range []*int{rule.MinItems, rule.MaxItems} {
			if n == nil {
				continue
			}
			for _, size := range []int{*n - 1, *n, *n + 1} {
				if size < 0 {
					continue
				}
				grown := []any{}
				for i := 0; i < size; i++ {
					grown = append(grown, "item")
				}
				out = append(out, grown)
			}
		}
		if len(list) > 0 {
			for _, value := range genericProbes() {
				swapped := append(cloneValue(list).([]any), value)
				out = append(out, swapped)
			}
		}
	}
	return out
}

// The equivalence test must see a decoder that changed: the two mutants
// that survived the vocabulary table's first tests, applied here to a copy of
// the hand-written decoder rather than to the source.
func TestTheEquivalenceTestSeesAChangedDecoder(t *testing.T) {
	ops, err := cloudvocab.Operations()
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]cloudvocab.Operation{}
	for _, row := range ops {
		rows[row.Op] = row
	}
	examples := map[string]any{}
	for name, mutant := range map[string]struct {
		word   string
		decode func(body) (plan, bool)
		key    string
	}{
		// archive-session starts accepting a field no row names.
		"a brand-new field": {word: "archive-session", key: "zzz_new", decode: func(b body) (plan, bool) {
			if _, ok := b["zzz_new"]; ok && len(b) == 5 {
				trimmed := cloneBody(b)
				delete(trimmed, "zzz_new")
				return catalog["archive-session"].decode(trimmed)
			}
			return catalog["archive-session"].decode(b)
		}},
		// project-tree-list's project bound drops from 200 to 100.
		"a changed limit": {word: "project-tree-list", decode: func(b body) (plan, bool) {
			if project, ok := b["project"].(string); ok && len(project) > 100 {
				return plan{}, false
			}
			return catalog["project-tree-list"].decode(b)
		}},
	} {
		o := catalog[mutant.word]
		o.decode = mutant.decode
		universe := []string{"brand_new_field"}
		if mutant.key != "" {
			universe = append(universe, mutant.key)
		}
		caught := false
		for _, b := range decoderProbes(rows[mutant.word], universe, examples, nil) {
			if compareDecoders(o, generatedDecoders[mutant.word], b) != "" {
				caught = true
				break
			}
		}
		if !caught {
			t.Errorf("%s: the equivalence test does not see %s changed", name, mutant.word)
		}
	}
	// And the mutant field is in the universe only because the source scan
	// would read it: the real source names no such key.
	if handWrittenBodyKeys(t)["zzz_new"] {
		t.Fatal("this package's source already spells zzz_new; pick a mutant key nothing spells")
	}
}
