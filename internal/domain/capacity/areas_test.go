package capacity

import "testing"

// A row declared in the wrong register_<area>.go is one a reviewer of that
// area never sees, and one its measure file does not look for; a prefix two
// areas both claim is a row with two homes.
func TestEveryRowIsDeclaredInItsArea(t *testing.T) {
	claimed := map[string]string{}
	for _, a := range areas {
		for _, p := range a.prefixes {
			if other, dup := claimed[p]; dup {
				t.Errorf("prefix %s is claimed by both %s and %s", p, other, a.name)
			}
			claimed[p] = a.name
		}
	}
	for _, a := range areas {
		rows := a.bounds()
		if len(rows) == 0 {
			t.Errorf("area %s declares no rows", a.name)
		}
		for _, e := range rows {
			if got := AreaOf(e.Name); got != a.name {
				t.Errorf("%s is declared in register_%s.go; its name places it in %q", e.Name, a.name, got)
			}
		}
	}
}
