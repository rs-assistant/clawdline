package schedulewebhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func investigationFixture(t *testing.T) ([]byte, InvestigationRef, Identity) {
	t.Helper()
	raw, err := os.ReadFile("testdata/investigation-bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var b InvestigationBundle
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	return raw, b.InvestigationRef, Identity{AccountID: "account_fixture", MachineID: b.Scope.MachineID}
}

func TestInvestigationFrozenBundleWire(t *testing.T) {
	raw, ref, identity := investigationFixture(t)
	if err := ValidateInvestigationBundle(raw, ref, identity); err != nil {
		t.Fatal(err)
	}
	at := append(bytes.Clone(raw), bytes.Repeat([]byte(" "), MaxInvestigationBundleBytes-len(raw))...)
	if err := ValidateInvestigationBundle(at, ref, identity); err != nil {
		t.Fatal("refused exact bundle byte boundary", err)
	}
	if err := ValidateInvestigationBundle(append(at, ' '), ref, identity); err == nil {
		t.Fatal("accepted bundle beyond byte boundary")
	}

	for _, name := range []string{"hash", "bundle_content", "scope_group", "revision", "investigation", "machine", "scope", "trailing", "bytes", "float", "duplicate"} {
		t.Run(name, func(t *testing.T) {
			changed := bytes.Clone(raw)
			r := ref
			id := identity
			switch name {
			case "hash":
				r.ContentHash = "sha256:" + strings.Repeat("0", 64)
			case "bundle_content":
				changed = bytes.Replace(changed, []byte(`"event": "timeout"`), []byte(`"event": "recovered"`), 1)
			case "scope_group":
				changed = bytes.Replace(changed, []byte(`"correlation": "fixture-operation"`), []byte(`"correlation": "other-operation"`), 1)
			case "revision":
				r.Revision++
			case "investigation":
				r.InvestigationID = "hiv_" + strings.Repeat("f", 32)
			case "machine":
				id.MachineID = "another_machine"
			case "scope":
				changed = bytes.Replace(changed, []byte(`"source_id": "fixture-source"`), []byte(`"source_id": "other-source"`), 1)
			case "trailing":
				changed = append(changed, []byte(` {}`)...)
			case "bytes":
				changed = append(changed, bytes.Repeat([]byte(" "), MaxInvestigationBundleBytes)...)
			case "float":
				changed = bytes.ReplaceAll(changed, []byte(`"revision": 3`), []byte(`"revision": 3.5`))
			case "duplicate":
				changed = bytes.Replace(changed, []byte(`"manifest": {`), []byte(`"manifest": {}, "manifest": {`), 1)
			}
			if err := ValidateInvestigationBundle(changed, r, id); err == nil {
				t.Fatal("accepted changed frozen evidence")
			}
		})
	}
}

func TestInvestigationReportClosedWire(t *testing.T) {
	raw := []byte(`{"schema":"clawdline.health_investigation.report.v1","investigation_id":"hiv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","conclusion":"fix_prepared","fix_commit":null,"evidence_refs":[]}`)
	if _, err := ParseInvestigationReport(raw); err != nil {
		t.Fatal(err)
	}
	at := append(bytes.Repeat([]byte(" "), MaxInvestigationReportBytes-len(raw)), raw...)
	if _, err := ParseInvestigationReport(at); err != nil {
		t.Fatal("refused exact report byte boundary", err)
	}
	if _, err := ParseInvestigationReport(append(at, ' ')); err == nil {
		t.Fatal("accepted report beyond byte boundary")
	}

	for _, bad := range [][]byte{
		bytes.Replace(raw, []byte(`"fix_prepared"`), []byte(`"recovered"`), 1),
		bytes.Replace(raw, []byte(`"fix_commit":null`), []byte(`"fix_commit":"/tmp/private"`), 1),
		bytes.Replace(raw, []byte(`"evidence_refs":[]`), []byte(`"evidence_refs":null`), 1),
		bytes.Replace(raw, []byte(`"evidence_refs":[]`), []byte(`"evidence_refs":[],"text":"secret"`), 1),
		append(bytes.Repeat([]byte(" "), MaxInvestigationReportBytes), raw...),
	} {
		if _, err := ParseInvestigationReport(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	refs := []string{}
	for i := 0; i < MaxInvestigationReportRefs; i++ {
		refs = append(refs, fmt.Sprintf("he_%040x", i+1))
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["evidence_refs"] = refs
	withRefs, _ := json.Marshal(m)
	if _, err := ParseInvestigationReport(withRefs); err != nil {
		t.Fatal(err)
	}
	m["evidence_refs"] = append(refs, "he_"+strings.Repeat("f", 40))
	over, _ := json.Marshal(m)
	if _, err := ParseInvestigationReport(over); err == nil {
		t.Fatal("accepted ninth or duplicate ref")
	}
}

func TestInvestigationCanonicalJSONUsesCloudEncoding(t *testing.T) {
	raw := []byte("{\"z\":\"<>&\\u2028\",\"a\":[1,true,null],\"😀\":2,\"\\ue000\":3}")
	got, err := CanonicalInvestigationJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":[1,true,null],\"z\":\"<>&\u2028\",\"😀\":2,\"\ue000\":3}"
	if string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	for _, bad := range []string{`{"x":1,"x":2}`, `{"x":1.5}`, `{"x":9007199254740992}`} {
		if _, err := CanonicalInvestigationJSON([]byte(bad)); err == nil {
			t.Fatal("accepted ambiguous canonical JSON")
		}
	}
}
