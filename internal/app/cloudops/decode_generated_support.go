package cloudops

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// What the generated decoders (zz_decode_generated.go) are written in. Each
// one is the vocabulary table's `decode` for its word, spelled out by
// tools/contract-gen; these are the few pieces they share, so that a rule in
// the table is read by one function here rather than by a hundred copies.
//
// Nothing in production calls a generated decoder yet:
// `TestGeneratedDecodersDecideAsTheHandWrittenOnes` holds each one equal to
// the hand-written decoder and route it will replace.

// genDecoder is one generated decoder: the plan's identity fields (where the
// answer goes and what it is pinned to), the local request, and whether the
// body was accepted at all.
type genDecoder func(b body) (plan, LocalRequest, bool)

// genRule is one body value's rule, as the table's `cloudvocab.Field` states it.
type genRule struct {
	kind       string
	nonEmpty   bool
	maxBytes   int
	length     int
	prefix     string
	min, max   *int64
	enum       []string
	equals     *bool
	checks     []func(string) bool
	nullable   bool
	sameAs     string
	items      *genRule
	minItems   *int
	maxItems   *int
	properties map[string]genRule
	required   []string
	closed     bool
	minKeys    int
	maxKeys    int
}

func genInt(v int64) *int64 { return &v }
func genCount(v int) *int   { return &v }
func genBool(v bool) *bool  { return &v }

// genNumber is a table constant number, held as a decoded body holds one.
func genNumber(v string) json.Number { return json.Number(v) }

// genValid is whether the body's value for key keeps its rule. An absent key
// keeps every rule: which keys must be present is the shapes' question.
func (b body) genValid(key string, r genRule) bool {
	v, present := b[key]
	return !present || genValueValid(b, v, r)
}

func genValueValid(b body, v any, r genRule) bool {
	if v == nil && r.nullable {
		return true
	}
	switch r.kind {
	case "any":
		return true
	case "string":
		s, ok := v.(string)
		if !ok || r.nonEmpty && s == "" || r.maxBytes > 0 && len(s) > r.maxBytes ||
			r.length > 0 && len(s) != r.length || !strings.HasPrefix(s, r.prefix) {
			return false
		}
		if len(r.enum) > 0 {
			known := false
			for _, candidate := range r.enum {
				known = known || s == candidate
			}
			if !known {
				return false
			}
		}
		for _, check := range r.checks {
			if !check(s) {
				return false
			}
		}
		if r.sameAs != "" {
			other, ok := b[r.sameAs].(string)
			return ok && other == s
		}
		return true
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		i, err := n.Int64()
		return err == nil && (r.min == nil || i >= *r.min) && (r.max == nil || i <= *r.max)
	case "bool":
		t, ok := v.(bool)
		return ok && (r.equals == nil || t == *r.equals)
	case "list":
		items, ok := v.([]any)
		if !ok || r.minItems != nil && len(items) < *r.minItems || r.maxItems != nil && len(items) > *r.maxItems {
			return false
		}
		for _, item := range items {
			if !genValueValid(b, item, *r.items) {
				return false
			}
		}
		return true
	case "object":
		object, ok := v.(map[string]any)
		if !ok || len(object) < r.minKeys || r.maxKeys > 0 && len(object) > r.maxKeys {
			return false
		}
		for _, key := range r.required {
			if _, present := object[key]; !present {
				return false
			}
		}
		for key, value := range object {
			rule, known := r.properties[key]
			if !known {
				if r.closed {
					return false
				}
				continue
			}
			if !genValueValid(b, value, rule) {
				return false
			}
		}
		if r.maxBytes > 0 {
			encoded, err := json.Marshal(object)
			return err == nil && len(encoded) <= r.maxBytes
		}
		return true
	}
	return false
}

// genText is a value as a path, a query or an answer name spells it.
func genText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return itoa(n)
		}
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	}
	return ""
}

// genValue is where one local value comes from (`cloudvocab.Value`).
type genValue struct {
	from      string
	constant  any
	fallback  any
	omitEmpty bool
	only      string
	ifTrue    string
	list      bool
}

// genSource is the value v names in b, and whether it is sent at all.
func (b body) genSource(v genValue) (any, bool) {
	out := v.constant
	if v.from != "" {
		raw, present := b[v.from]
		if !present {
			if v.fallback == nil {
				return nil, false
			}
			raw = v.fallback
		}
		out = raw
	}
	if v.omitEmpty {
		switch t := out.(type) {
		case string:
			if t == "" {
				return nil, false
			}
		case bool:
			if !t {
				return nil, false
			}
		case json.Number:
			if genText(t) == "0" {
				return nil, false
			}
		}
	}
	if v.only != "" && genText(out) != v.only {
		return nil, false
	}
	if v.ifTrue != "" {
		if t, _ := out.(bool); !t {
			return nil, false
		}
		out = v.ifTrue
	}
	if v.list {
		out = []any{out}
	}
	return out, true
}

// genQuery puts each sent value into a route's query.
func (b body) genQuery(values map[string]genValue) map[string]string {
	out := map[string]string{}
	for key, v := range values {
		if value, sent := b.genSource(v); sent {
			out[key] = genText(value)
		}
	}
	return out
}

// genFields is a local JSON body built from values.
func (b body) genFields(values map[string]genValue) []byte {
	out := map[string]any{}
	for key, v := range values {
		if value, sent := b.genSource(v); sent {
			out[key] = value
		}
	}
	return jsonBody(out)
}

// genObject is a body field's object as the local body, as it arrived.
func (b body) genObject(key string) []byte {
	out, err := json.Marshal(b[key])
	if err != nil {
		return []byte("{}")
	}
	return out
}

// The named checks of `cloudvocab.Checks`, each over a string.

func genRequestName(s string) bool { _, ok := requestName(s); return ok }
func genSessionName(s string) bool { _, ok := sessionName(s); return ok }
func genFingerprint(s string) bool { return session.ValidFingerprint(s) }
func genDocumentPath(s string) bool {
	_, ok := documentPath(s)
	return ok
}
func genTreeFile(s string) bool {
	_, err := projectfiles.TreePath(s, true)
	return err == nil
}
func genTreeDirectory(s string) bool {
	_, err := projectfiles.TreePath(s, false)
	return err == nil
}
func genUsageID(s string) bool        { return usageID.MatchString(s) && !strings.Contains(s, "..") }
func genVerificationID(s string) bool { return verificationID.MatchString(s) }
func genCompareSince(s string) bool   { return compareSince.MatchString(s) }
func genWorkUntil(s string) bool      { return workUntil.MatchString(s) }
func genAbsolutePath(s string) bool   { return filepath.IsAbs(s) }
func genNotBlank(s string) bool       { return strings.TrimSpace(s) != "" }
func genLowerHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
