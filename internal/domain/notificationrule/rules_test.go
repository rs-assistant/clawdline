package notificationrule

import (
	"encoding/json"
	"testing"
)

func TestMatchingRulesChooseShortestEnabledWait(t *testing.T) {
	rules := []Rule{
		{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Enabled: true, AfterMinutes: 30, LabelContains: "project"},
		{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Enabled: true, AfterMinutes: 3, LabelContains: "urgent"},
		{ID: "cccccccccccccccccccccccccccccccc", Enabled: false, AfterMinutes: 1, LabelContains: ""},
	}
	if got, ok := Match(rules, "Urgent project"); !ok || got.AfterMinutes != 3 {
		t.Fatalf("selected %+v, %v", got, ok)
	}
	if _, ok := Match(rules, "ordinary Session"); ok {
		t.Fatal("unmatched rule fired")
	}
	for _, raw := range []string{
		`null`, `[{}]`,
		`[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","enabled":true,"after_minutes":0,"label_contains":""}]`,
		`[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","enabled":true,"after_minutes":3,"label_contains":" a "}]`,
	} {
		if _, err := Decode(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
