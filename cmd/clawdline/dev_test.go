package main

import "testing"

func TestADevBuildIsNamedForTheReleaseItGoesOver(t *testing.T) {
	commit := "4c41b18ae9f0ecd4daccc1b55dc044fe7b792f80"
	for running, want := range map[string]string{
		"v1.0.7":             "v1.0.7-dev.4c41b18",
		"v1.0.7-dev.1234567": "v1.0.7-dev.4c41b18",
	} {
		if got, err := devVersion(running, commit); err != nil || got != want {
			t.Errorf("over %s: %q, %v; want %q", running, got, err, want)
		}
	}
	if _, err := devVersion("v1.1.0-beta.2", commit); err == nil {
		t.Error("a dev build went over a beta, which it would then follow")
	}
}
