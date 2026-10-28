package main

import (
	"bytes"
	"strings"
	"testing"

	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// A revocation replaced the content key. The terminal says so, says that the
// other browsers fetch the new key and which ones must pair again, and says
// it in Traditional Chinese too.
func TestARevocationSaysWhichBrowsersMustPairAgain(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	outcome := cloudtransport.RevokeOutcome{Device: "dev_gone", Revoked: true, SecretReplaced: true}
	for _, tc := range []struct{ language, replaced, repair string }{
		{"en", "content key was replaced", "fetch the new key on their own"},
		{"zh-Hant", "內容金鑰已更換", "會自動取得新金鑰"},
	} {
		commandLanguage = tc.language
		var out bytes.Buffer
		printRevokeOutcome(&out, outcome)
		text := out.String()
		for _, want := range []string{"dev_gone", tc.replaced, tc.repair, "clawdline cloud pair"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the revocation output lacks %q:\n%s", tc.language, want, text)
			}
		}
	}
	commandLanguage = "en"
	var kept bytes.Buffer
	printRevokeOutcome(&kept, cloudtransport.RevokeOutcome{Device: "dev_gone", Revoked: true})
	if !strings.Contains(kept.String(), "was NOT replaced") {
		t.Errorf("a revocation without a replaced key read as complete:\n%s", kept.String())
	}
}
