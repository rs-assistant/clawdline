package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The admin connection-health page keeps its own bounds in one literal
// (web/console/src/cloud/admin/health/cloud.ts HEALTH_UI_LIMITS). Every key of
// it is a registered row with the same value, and every row names a key that
// is there: a bound added or changed on one side only fails here.
func TestConnectionHealthUIBoundsMatchThePage(t *testing.T) {
	root := moduleRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "web", "console", "src", "cloud", "admin", "health", "cloud.ts"))
	if err != nil {
		t.Fatalf("read cloud.ts: %v", err)
	}
	src := string(raw)
	start := strings.Index(src, "export const HEALTH_UI_LIMITS = {")
	if start < 0 {
		t.Fatal("HEALTH_UI_LIMITS is not in cloud.ts")
	}
	end := strings.Index(src[start:], "} as const")
	if end < 0 {
		t.Fatal("HEALTH_UI_LIMITS is not closed in cloud.ts")
	}
	block := src[start : start+end]
	values := map[string]int64{}
	for _, m := range regexp.MustCompile(`(?m)^\s+(\w+): ([0-9_ *]+|STORED_BOUNDS\.pageRows),`).FindAllStringSubmatch(block, -1) {
		if m[2] == "STORED_BOUNDS.pageRows" {
			values[m[1]] = 200
			continue
		}
		product := int64(1)
		for _, factor := range strings.Split(m[2], "*") {
			n, err := strconv.ParseInt(strings.ReplaceAll(strings.TrimSpace(factor), "_", ""), 10, 64)
			if err != nil {
				t.Fatalf("HEALTH_UI_LIMITS.%s: %q is not a literal product", m[1], m[2])
			}
			product *= n
		}
		values[m[1]] = product
	}
	// key → row, and the divisor from the page's unit to the row's.
	want := map[string]struct {
		row   string
		scale int64
	}{
		"requestMs":           {ConsoleHealthUIRequestSeconds, 1000},
		"budgetMs":            {ConsoleHealthUIBudgetSeconds, 1000},
		"parallel":            {ConsoleHealthUIParallelReads, 1},
		"readsPerLoad":        {ConsoleHealthUIReadsPerLoad, 1},
		"refreshMs":           {ConsoleHealthUIRefreshSeconds, 1000},
		"bodyBytes":           {ConsoleHealthUIBodyBytes, 1},
		"pageRows":            {ConsoleHealthUIPageRows, 1},
		"eventPages":          {ConsoleHealthUIEventPages, 1},
		"incidentRows":        {ConsoleHealthUIIncidentRows, 1},
		"incidentExportBytes": {ConsoleHealthUIIncidentExportBytes, 1},
		"incidentWindowMs":    {ConsoleHealthUIIncidentWindowSeconds, 1000},
		"incidentPages":       {ConsoleHealthUIIncidentPages, 1},
		"trendLanes":          {ConsoleHealthUITrendLanes, 1},
		"trendSignals":        {ConsoleHealthUITrendSignals, 1},
	}
	rows := map[string]Entry{}
	for _, e := range Register() {
		rows[e.Name] = e
	}
	for key := range values {
		if _, ok := want[key]; !ok {
			t.Errorf("HEALTH_UI_LIMITS.%s has no registered row", key)
		}
	}
	for key, w := range want {
		v, ok := values[key]
		if !ok {
			t.Errorf("%s names HEALTH_UI_LIMITS.%s, which cloud.ts does not declare", w.row, key)
			continue
		}
		e, ok := rows[w.row]
		if !ok {
			t.Errorf("%s (HEALTH_UI_LIMITS.%s = %d) is not registered", w.row, key, v)
			continue
		}
		if e.Limit != v/w.scale {
			t.Errorf("%s is %d, HEALTH_UI_LIMITS.%s says %d", w.row, e.Limit, key, v/w.scale)
		}
	}
}
