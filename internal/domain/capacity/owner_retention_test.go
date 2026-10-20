package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Settings → Remote's owner-diagnostics block keeps its bounds in one literal
// (web/console/src/cloud/health-owner/retention.ts OWNER_RETENTION_LIMITS).
// Every key of it is a registered row with the same value, and every row
// names a key that is there.
func TestOwnerRetentionBoundsMatchTheBlock(t *testing.T) {
	root := moduleRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "web", "console", "src", "cloud", "health-owner", "retention.ts"))
	if err != nil {
		t.Fatalf("read retention.ts: %v", err)
	}
	src := string(raw)
	start := strings.Index(src, "export const OWNER_RETENTION_LIMITS = {")
	if start < 0 {
		t.Fatal("OWNER_RETENTION_LIMITS is not in retention.ts")
	}
	end := strings.Index(src[start:], "} as const")
	if end < 0 {
		t.Fatal("OWNER_RETENTION_LIMITS is not closed by `} as const`")
	}
	values := map[string]int64{}
	for _, m := range regexp.MustCompile(`(?m)^\s+(\w+): ([0-9_]+),`).FindAllStringSubmatch(src[start:start+end], -1) {
		n, _ := strconv.ParseInt(strings.ReplaceAll(m[2], "_", ""), 10, 64)
		values[m[1]] = n
	}
	want := map[string]string{
		"pollMs":    ConsoleOwnerRetentionPollSeconds,
		"requestMs": ConsoleOwnerRetentionRequestSeconds,
	}
	rows := map[string]Entry{}
	for _, e := range Register() {
		rows[e.Name] = e
	}
	for key := range values {
		if _, ok := want[key]; !ok {
			t.Errorf("OWNER_RETENTION_LIMITS.%s has no registered row", key)
		}
	}
	for key, row := range want {
		v, ok := values[key]
		if !ok {
			t.Errorf("%s names OWNER_RETENTION_LIMITS.%s, which retention.ts does not declare", row, key)
			continue
		}
		e, ok := rows[row]
		if !ok {
			t.Errorf("%s (OWNER_RETENTION_LIMITS.%s = %d ms) is not registered", row, key, v)
			continue
		}
		if e.Limit*1000 != v {
			t.Errorf("%s is %d s, OWNER_RETENTION_LIMITS.%s says %d ms", row, e.Limit, key, v)
		}
	}
}
