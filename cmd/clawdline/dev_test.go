package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

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

type sourceDevRunner struct {
	calls []string
	err   error
}

func (r *sourceDevRunner) run(dir string, out, stderr io.Writer, name string, args ...string) error {
	r.calls = append(r.calls, dir+" "+name+" "+strings.Join(args, " "))
	return r.err
}
func TestSourceDevUsesTheSameEntryAndPreservesDeploymentFailures(t *testing.T) {
	for _, tc := range []struct {
		name, platform string
		port           int
		failure        error
		want, calls    int
	}{
		{"Linux source to dev", "linux", 0, nil, 0, 1},
		{"source dev to next dev", "linux", 0, nil, 0, 1},
		{"build or rollback failed", "linux", 0, errors.New("build failed before switch"), 1, 1},
		{"unowned port", "linux", 8899, nil, 1, 0},
		{"unsupported service", "darwin", 0, nil, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &sourceDevRunner{err: tc.failure}
			var out, stderr bytes.Buffer
			code := devSourceInstall(&out, &stderr, "/repo", "abcdef1234567", tc.platform, tc.port, r)
			if code != tc.want || len(r.calls) != tc.calls {
				t.Fatalf("code %d, calls %v, error %s", code, r.calls, stderr.String())
			}
			if tc.calls == 1 && r.calls[0] != "/repo bash /repo/tools/deploy-linux-user.sh --dev --rev abcdef1234567" {
				t.Fatal(r.calls)
			}
			if tc.want != 0 && stderr.Len() == 0 {
				t.Fatal("failure had no explanation")
			}
		})
	}
}
