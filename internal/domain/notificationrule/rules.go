// Package notificationrule defines user-authored conditions for Session waiting pushes.
package notificationrule

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const FileKey = "notification_rules"
const MaximumRules = 16
const MaximumLabelRunes = 80
const MinMinutes = 0
const MaxMinutes = 120

var ErrInvalid = errors.New("invalid notification rule")
var ErrUnsupportedDescription = errors.New("unsupported notification situation")

var chineseSituation = regexp.MustCompile(`^(?:當|如果)\s*(?:名稱\s*(?:包含|含有)\s*[「"]([^」"]+)[」"]\s*的\s*)?(?:Session|session|工作階段)\s*(?:等我回答|需要我回答|等我授權|需要我授權)(?:\s*(?:超過|等待)\s*([0-9]{1,3})\s*分鐘)?\s*(?:時|就)?\s*(?:通知我|推播給我)[。！!]?$`)
var englishSituation = regexp.MustCompile(`(?i)^notify me when (?:a )?session(?: named "([^"]+)")? (?:needs my answer|needs my permission)(?: for ([0-9]{1,3}) minutes?)?[.!]?$`)

type Rule struct {
	ID            string `json:"id"`
	Enabled       bool   `json:"enabled"`
	AfterMinutes  int    `json:"after_minutes"`
	LabelContains string `json:"label_contains"`
}

// ParseSituation accepts only situations the waiting watcher can actually observe.
// An unrecognized sentence is refused instead of becoming a rule that never fires.
func ParseSituation(description string) (Rule, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return Rule{}, ErrUnsupportedDescription
	}
	parts := chineseSituation.FindStringSubmatch(description)
	if parts == nil {
		parts = englishSituation.FindStringSubmatch(description)
	}
	if parts == nil {
		return Rule{}, ErrUnsupportedDescription
	}
	label := strings.TrimSpace(parts[1])
	minutes := 0
	if parts[2] != "" {
		var err error
		minutes, err = strconv.Atoi(parts[2])
		if err != nil {
			return Rule{}, ErrUnsupportedDescription
		}
	}
	rule := Rule{Enabled: true, AfterMinutes: minutes, LabelContains: label}
	if label != parts[1] || utf8.RuneCountInString(label) > MaximumLabelRunes || minutes > MaxMinutes {
		return Rule{}, ErrUnsupportedDescription
	}
	return rule, nil
}

func (r Rule) Valid() bool {
	if len(r.ID) != 32 {
		return false
	}
	for _, c := range r.ID {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return r.AfterMinutes >= MinMinutes && r.AfterMinutes <= MaxMinutes &&
		utf8.RuneCountInString(r.LabelContains) <= MaximumLabelRunes && strings.TrimSpace(r.LabelContains) == r.LabelContains
}

func Decode(raw json.RawMessage) ([]Rule, error) {
	if len(raw) == 0 {
		return []Rule{}, nil
	}
	var rules []Rule
	if err := json.Unmarshal(raw, &rules); err != nil || rules == nil || len(rules) > MaximumRules {
		return nil, ErrInvalid
	}
	ids := make(map[string]bool, len(rules))
	for _, r := range rules {
		if !r.Valid() || ids[r.ID] {
			return nil, ErrInvalid
		}
		ids[r.ID] = true
	}
	return rules, nil
}

// Match chooses the soonest enabled rule whose case-insensitive label filter matches.
func Match(rules []Rule, label string) (Rule, bool) {
	var chosen Rule
	found := false
	for _, r := range rules {
		if !r.Enabled || !r.Valid() || !strings.Contains(strings.ToLower(label), strings.ToLower(r.LabelContains)) {
			continue
		}
		if !found || r.AfterMinutes < chosen.AfterMinutes {
			chosen, found = r, true
		}
	}
	return chosen, found
}
