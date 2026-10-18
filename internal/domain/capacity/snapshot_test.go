package capacity

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/register.golden.json from the register")

// snapshotRow is one registered row as data, with the register it came from.
// Entry is embedded whole so a field added to it shows up in the snapshot
// without anybody remembering to add it here.
type snapshotRow struct {
	Register string
	Entry
}

// renderRegister is every row of Register and ToolRegister, sorted by name,
// as indented JSON. Nothing in it depends on the machine or the clock.
func renderRegister(t *testing.T) []byte {
	t.Helper()
	var rows []snapshotRow
	for _, e := range Register() {
		rows = append(rows, snapshotRow{Register: "daemon", Entry: e})
	}
	for _, e := range ToolRegister() {
		rows = append(rows, snapshotRow{Register: "tool", Entry: e})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].Register < rows[j].Register
	})
	out, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(out, '\n')
}

// The register is pinned byte for byte, so moving a row's declaration next
// to the package that owns it is proved to change no row. Regenerate with
// `go test ./internal/domain/capacity -run TestTheRegisterMatchesItsSnapshot -update`
// only when a row is meant to change.
func TestTheRegisterMatchesItsSnapshot(t *testing.T) {
	got := renderRegister(t)
	path := filepath.Join("testdata", "register.golden.json")
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run with -update to create it", err)
	}
	if bytes.Equal(got, want) {
		return
	}
	var gotRows, wantRows []map[string]any
	if err := json.Unmarshal(got, &gotRows); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &wantRows); err != nil {
		t.Fatalf("%s is not a register snapshot: %v", path, err)
	}
	byName := func(rows []map[string]any) map[string]string {
		m := map[string]string{}
		for _, r := range rows {
			b, _ := json.Marshal(r)
			m[r["Register"].(string)+" "+r["Name"].(string)] = string(b)
		}
		return m
	}
	g, w := byName(gotRows), byName(wantRows)
	var keys []string
	for k := range g {
		keys = append(keys, k)
	}
	for k := range w {
		if _, ok := g[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch gv, gok := g[k]; {
		case !gok:
			t.Errorf("%s is in the snapshot and no longer registered", k)
		case w[k] == "":
			t.Errorf("%s is registered and not in the snapshot", k)
		case gv != w[k]:
			t.Errorf("%s changed:\n got  %s\n want %s", k, gv, w[k])
		}
	}
	if !t.Failed() {
		t.Errorf("%s differs from the register in order or formatting; regenerate it with -update", path)
	}
}
