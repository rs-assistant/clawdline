package capacity

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The browser's connection-health producer keeps its bounds in one literal
// (web/console/src/health-viewer/producer.ts VIEWER_HEALTH_LIMITS). Every key
// of it is a registered row with the same value, and every row names a key
// that is there: a bound changed on one side only fails here.
func TestViewerHealthBoundsMatchTheProducer(t *testing.T) {
	root := moduleRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "web", "console", "src", "health-viewer", "producer.ts"))
	if err != nil {
		t.Fatalf("read producer: %v", err)
	}
	src := string(raw)
	start := strings.Index(src, "export const VIEWER_HEALTH_LIMITS = {")
	end := strings.Index(src[start:], "} as const")
	if start < 0 || end < 0 {
		t.Fatal("VIEWER_HEALTH_LIMITS is not in producer.ts")
	}
	block := src[start : start+end]
	values := map[string]int64{}
	for _, m := range regexp.MustCompile(`(?m)^\s+(\w+): ([0-9_]+),`).FindAllStringSubmatch(block, -1) {
		n, _ := strconv.ParseInt(strings.ReplaceAll(m[2], "_", ""), 10, 64)
		values[m[1]] = n
	}
	if m := regexp.MustCompile(`backoffMs: \[([0-9_, ]+)\]`).FindStringSubmatch(block); m != nil {
		for _, part := range strings.Split(m[1], ",") {
			n, _ := strconv.ParseInt(strings.ReplaceAll(strings.TrimSpace(part), "_", ""), 10, 64)
			if n > values["backoffMs"] {
				values["backoffMs"] = n
			}
		}
	}
	// key → row, and the divisor from the producer's unit to the row's.
	want := map[string]struct {
		row   string
		scale int64
	}{
		"bodyBytes":        {ConsoleViewerHealthBodyBytes, 1},
		"batchEvents":      {ConsoleViewerHealthBatchRows, 1},
		"bufferEvents":     {ConsoleViewerHealthBufferRows, 1},
		"sendEveryMs":      {ConsoleViewerHealthSendEverySeconds, 1000},
		"coalesceMs":       {ConsoleViewerHealthCoalesceSeconds, 1000},
		"hourlyRequests":   {ConsoleViewerHealthHourlyRequests, 1},
		"hourlyBytes":      {ConsoleViewerHealthHourlyBytes, 1},
		"attempts":         {ConsoleViewerHealthAttempts, 1},
		"backoffMs":        {ConsoleViewerHealthBackoffSeconds, 1000},
		"retryAfterMaxMs":  {ConsoleViewerHealthRetryAfterSeconds, 1000},
		"requestTimeoutMs": {ConsoleViewerHealthRequestSeconds, 1000},
		"elapsedMaxMs":     {ConsoleViewerHealthElapsedSeconds, 1000},
		"countMax":         {ConsoleViewerHealthCountRows, 1},
	}
	rows := map[string]Entry{}
	for _, e := range Register() {
		rows[e.Name] = e
	}
	for key := range values {
		if _, ok := want[key]; !ok {
			t.Errorf("VIEWER_HEALTH_LIMITS.%s has no registered row", key)
		}
	}
	for key, w := range want {
		v, ok := values[key]
		if !ok {
			t.Errorf("%s names VIEWER_HEALTH_LIMITS.%s, which producer.ts does not declare", w.row, key)
			continue
		}
		e, ok := rows[w.row]
		if !ok {
			t.Errorf("%s (VIEWER_HEALTH_LIMITS.%s = %d) is not registered", w.row, key, v)
			continue
		}
		if e.Limit != v/w.scale {
			t.Errorf("%s is %d, VIEWER_HEALTH_LIMITS.%s says %d", w.row, e.Limit, key, v/w.scale)
		}
	}
}
