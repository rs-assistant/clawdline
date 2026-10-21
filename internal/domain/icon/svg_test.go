package icon

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSVGIconsValidateAndSurviveRegistryRestart(t *testing.T) {
	source := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"><defs><linearGradient id="a"><stop stop-color="#ffffff"/></linearGradient></defs><path fill="url(#a)" d="M0 0h100v50z"/></svg>`
	g := Grid{Accent: "#ABCDEF", SVG: source}
	if err := Validate(g); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistryWithOverrides(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Save("/example/project", g, Generated("/example/project")); err != nil {
		t.Fatal(err)
	}
	next, err := NewRegistryWithOverrides(strings.TrimSuffix(r.overridePath, "/project-icon-overrides.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := next.For("/example/project/sub"); got.SVG != source {
		t.Fatal("SVG did not survive restart")
	}
	legacy := registryAt(t, `{"projects":{"/example/project":{"art":{"accent":"#ABCDEF","svg":`+quoteJSON(t, source)+`}}}}`)
	if got := legacy.For("/example/project/sub"); got.SVG != source {
		t.Fatal("legacy registry lost SVG")
	}
}

func TestSVGIconsRejectActiveExternalMalformedAndOversizedContent(t *testing.T) {
	for _, source := range []string{
		`<svg viewBox="0 0 1 1"><script/></svg>`,
		`<svg viewBox="0 0 1 1" onload="alert(1)"/>`,
		`<svg viewBox="0 0 1 1"><use href="https://example.com/a.svg#b"/></svg>`,
		`<svg viewBox="0 0 1 1"><path fill="url(https://example.com)"/></svg>`,
		`<svg viewBox="0 0 1 1"><foreignObject/></svg>`,
		`<!DOCTYPE svg><svg viewBox="0 0 1 1"/>`,
		`<svg viewBox="0 0 0 1"/>`, `<svg viewBox="0 0 NaN 1"/>`,
		`<svg/>`, `<svg viewBox="0 0 1 1"/> <svg viewBox="0 0 1 1"/>`,
		`<svg viewBox="0 0 1 1">` + strings.Repeat(" ", MaxIconRequestBytes) + `</svg>`,
	} {
		if err := Validate(Grid{Accent: "#ABCDEF", SVG: source}); err == nil {
			t.Errorf("accepted invalid SVG: %.80s", source)
		}
	}
	if err := Validate(Grid{Accent: "#ABCDEF", SVG: `<svg viewBox="0 0 1 1"/>`, Cells: [][]*string{{nil}}}); err == nil {
		t.Fatal("accepted ambiguous SVG and pixels")
	}
}

func TestLegacyArtRejectsTooManyRowsAndColumns(t *testing.T) {
	for _, rows := range [][]string{make([]string, MaxIconSide+1), {strings.Repeat("x", MaxIconSide+1)}} {
		if _, ok := artGrid(art{Accent: "#ABCDEF", Rows: rows}); ok {
			t.Fatal("accepted oversized legacy art")
		}
	}
}

func quoteJSON(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
