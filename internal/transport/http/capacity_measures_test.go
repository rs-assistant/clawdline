package http

import (
	"slices"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// Each capacity_measures_<area>.go measures only rows its register area
// declares, so a measure cannot drift away from the bound it reads, and no
// row is measured by two files, where the merge would keep only one.
func TestEveryMeasureIsInItsBoundsArea(t *testing.T) {
	var areas []string
	for _, a := range capacityMeasureAreas {
		areas = append(areas, a.area)
	}
	if want := capacity.Areas(); !slices.Equal(areas, want) {
		t.Errorf("measure areas %v, register areas %v", areas, want)
	}
	registered := map[string]bool{}
	for _, e := range capacity.Register() {
		registered[e.Name] = true
	}
	s := capacityServer(t)
	measuredIn := map[string]string{}
	for _, a := range capacityMeasureAreas {
		for name := range a.measures(s) {
			if !registered[name] {
				t.Errorf("capacity_measures_%s.go measures %s, which is not a registered row", a.area, name)
			}
			if got := capacity.AreaOf(name); got != a.area {
				t.Errorf("%s is measured in capacity_measures_%s.go; its bound is in area %q", name, a.area, got)
			}
			if other, dup := measuredIn[name]; dup {
				t.Errorf("%s is measured in both %s and %s", name, other, a.area)
			}
			measuredIn[name] = a.area
		}
	}
}
