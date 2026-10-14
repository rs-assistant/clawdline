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
		`[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","enabled":true,"after_minutes":121,"label_contains":""}]`,
		`[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","enabled":true,"after_minutes":3,"label_contains":" a "}]`,
	} {
		if _, err := Decode(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestPlainLanguageSituationOnlyAcceptsObservableWaiting(t *testing.T) {
	for _, example := range []struct {
		text    string
		minutes int
		label   string
	}{
		{"當 Session 等我回答時通知我", 0, ""},
		{"當名稱包含「部署」的 Session 等我回答超過 5 分鐘時通知我", 5, "部署"},
		{"當 Session 需要我授權時推播給我", 0, ""},
		{"Notify me when a Session needs my answer for 5 minutes.", 5, ""},
		{`Notify me when a Session named "deploy" needs my permission.`, 0, "deploy"},
	} {
		rule, err := ParseSituation(example.text)
		if err != nil || rule.AfterMinutes != example.minutes || rule.LabelContains != example.label || !rule.Enabled {
			t.Fatalf("%q => %+v, %v", example.text, rule, err)
		}
	}
	for _, text := range []string{"當 Session 完成時通知我", "當排程失敗時通知我", "當 Session 等我回答超過 121 分鐘時通知我", "提醒我", "當 Session 等我回答時不要通知我"} {
		if _, err := ParseSituation(text); err == nil {
			t.Fatalf("unsupported situation accepted: %q", text)
		}
	}
}
