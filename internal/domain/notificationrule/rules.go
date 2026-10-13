// Package notificationrule defines user-authored conditions for Session waiting pushes.
package notificationrule

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const FileKey = "notification_rules"
const MaximumRules = 16
const MaximumLabelRunes = 80
const MinMinutes = 1
const MaxMinutes = 120

var ErrInvalid = errors.New("invalid notification rule")

type Rule struct {
	ID            string `json:"id"`
	Enabled       bool   `json:"enabled"`
	AfterMinutes  int    `json:"after_minutes"`
	LabelContains string `json:"label_contains"`
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
