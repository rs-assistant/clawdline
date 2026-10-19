package http

import (
	"encoding/json"
	"testing"
)

func TestUsageOverviewRouteValidatesAndPreservesNullGaps(t *testing.T) {
	u := newUsageFixture(t)
	for _, path := range []string{"/v1/usage/overview?days=8", "/v1/usage/overview?days=7&days=28", "/v1/usage/overview?since=1", "/v1/usage/overview?project=unknown"} {
		status, raw := u.get(path, nil)
		if status != 400 {
			t.Fatalf("%s: %d %s", path, status, raw)
		}
	}
	status, raw := u.get("/v1/usage/overview?days=28", nil)
	if status != 200 {
		t.Fatalf("%d %s", status, raw)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got["daily"].([]any)) != 28 || got["totals"].(map[string]any)["tokens"] != nil {
		t.Fatalf("%s", raw)
	}
	status, _ = u.get("/v1/usage/overview", map[string]string{})
	if status == 200 {
		t.Fatal("unauthenticated usage disclosed")
	}
	if err := u.s.store.Close(); err != nil {
		t.Fatal(err)
	}
	status, raw = u.get("/v1/usage/overview", nil)
	var refusal map[string]any
	json.Unmarshal(raw, &refusal)
	if status != 503 || refusal["error"] != "store_unavailable" {
		t.Fatalf("store refusal %d %s", status, raw)
	}
}
