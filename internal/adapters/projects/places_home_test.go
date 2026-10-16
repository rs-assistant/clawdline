package projects

import (
	"testing"
)

// The home directory is not offered just because a session started there, but
// once the person adds it, it is listed: on 2026-10-16 a fresh Linux machine's
// folder picker offered ~, the add answered success, and the dialog went on
// saying there were no projects.
func TestAnAddedHomeDirectoryIsListed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	listed := func(p *Places) bool {
		for _, place := range p.List([]string{home}, 10) {
			if place.Path == home {
				return true
			}
		}
		return false
	}
	p := NewPlaces(func(string) string { return "" }, nil)
	if listed(p) {
		t.Fatal("the home directory is listed only because a session runs there")
	}
	p.Registered = func() ([]RegisteredPlace, error) {
		return []RegisteredPlace{{Path: home, AddedAt: 1}}, nil
	}
	if !listed(p) {
		t.Fatal("the home directory the person added is not listed")
	}
}
