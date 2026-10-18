package cloudops

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
)

// planIdentity is the answer identity, and every other plan field the
// production code reads for o once the local request is built: a guard's
// key, question and whether it names typed words, a shaped picture's id, an
// info read's parts (the content read rail admits only full), a viewer event
// batch.
func planIdentity(o op, p plan) string {
	out := fmt.Sprintf("session=%q name=%q request=%q target=%q generation=%q",
		p.session, p.name, p.request, p.target, p.executionGeneration)
	if o.guard != nil {
		out += fmt.Sprintf(" key=%q expect=%q typed=%v", p.key, p.expect, p.namesTyped)
	}
	if o.shape != nil {
		out += fmt.Sprintf(" id=%q", p.id)
	}
	if o.name == "info" {
		out += fmt.Sprintf(" parts=%q", p.parts)
	}
	if o.name == viewerEventsWord {
		out += fmt.Sprintf(" document=%q", p.document)
	}
	return out
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
// each key a recorded body holds, and one name nothing anywhere spells.
func bodyKeyUniverse(ops []cloudvocab.Operation, recorded []goldenCase) ([]string, error) {
	keys := map[string]bool{"brand_new_field": true}
	for _, row := range ops {
		for _, shape := range row.Shapes {
			for _, field := range shape.Body {
				keys[field] = true
			}
		}
	}
	for _, c := range recorded {
		b, err := goldenBody(c.Body)
		if err != nil {
			return nil, err
		}
		for key := range b {
			keys[key] = true
		}
	}
	delete(keys, "type")
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

// decoderProbes is every body one row is tried with.
func decoderProbes(row cloudvocab.Operation, universe []string, examples map[string]any) []body {
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

// The golden must see a decoder that changed: the two mutants that survived
// the vocabulary table's first tests, applied here to a copy of the served
// decoder rather than to the source.
func TestTheGoldenSeesAChangedDecoder(t *testing.T) {
	recorded := map[string][]goldenCase{}
	for _, c := range readDecoderGolden(t) {
		recorded[c.Op] = append(recorded[c.Op], c)
	}
	for name, mutant := range map[string]struct {
		word   string
		decode func(body) (plan, bool)
	}{
		// archive-session starts accepting a field no row names.
		"a brand-new field": {word: "archive-session", decode: func(b body) (plan, bool) {
			if _, ok := b["brand_new_field"]; ok && len(b) == 5 {
				trimmed := cloneBody(b)
				delete(trimmed, "brand_new_field")
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
		caught := false
		for _, c := range recorded[mutant.word] {
			b, err := goldenBody(c.Body)
			if err != nil {
				t.Fatal(err)
			}
			got, err := goldenAnswer(o, served(o), c.Op, b)
			if err != nil {
				t.Fatal(err)
			}
			if goldenDiff(got, c) != "" {
				caught = true
				break
			}
		}
		if !caught {
			t.Errorf("%s: the golden does not see %s changed", name, mutant.word)
		}
	}
}
