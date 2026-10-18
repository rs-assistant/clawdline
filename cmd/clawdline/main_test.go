package main

import "testing"

func TestBuildRevisionKeepsThePackagingCommitWithoutVCSMetadata(t *testing.T) {
	previous := releaseCommit
	t.Cleanup(func() { releaseCommit = previous })
	const commit = "abcdef1234567890abcdef1234567890abcdef12"
	releaseCommit = commit
	if got := buildRevision(); got != commit {
		t.Fatalf("buildRevision() = %q, want packaging commit %q", got, commit)
	}
}
