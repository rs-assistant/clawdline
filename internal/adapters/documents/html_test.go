package documents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTMLPreservesStaticReport(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "report.html"), staticHTMLFixture)
	located, media, body, err := Read(root, "report.html")
	if err != nil || media != "text/html; charset=utf-8" || located.Bytes != int64(len(body)) {
		t.Fatalf("read: %+v %q %v", located, media, err)
	}
	for _, want := range []string{"<style>", "color: #123", "display: grid", `style="padding: 8px;"`, "<svg", "viewbox=", "<rect", "<table>", "<details>", "<summary>More</summary>"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("missing %q in %s", want, body)
		}
	}
	t.Logf("input_bytes=%d output_bytes=%d output=%s", len(staticHTMLFixture), len(body), body)
}

func TestHTMLRemovesActiveAndExternalContent(t *testing.T) {
	for _, attack := range []string{
		`<script>alert(1)</script><p onclick="alert(1)">safe</p>`,
		`<meta http-equiv="refresh" content="0;url=https://evil.invalid"><base href="https://evil.invalid"><a href="https://evil.invalid" target="_top" ping="https://evil.invalid" download>safe</a>`,
		`<iframe srcdoc="<script>alert(1)</script>"></iframe><object data="https://evil.invalid"></object><embed src="https://evil.invalid">`,
		`<form action="https://evil.invalid"><input formaction="https://evil.invalid"><button>send</button></form>`,
		`<link rel="stylesheet" href="https://evil.invalid"><img src="https://evil.invalid" srcset="//evil.invalid 2x"><video poster="https://evil.invalid"></video>`,
		`<svg onload="alert(1)"><foreignObject><iframe src="https://evil.invalid"></iframe></foreignObject><animate attributeName="href" to="https://evil.invalid"/><use href="https://evil.invalid"/><image href="https://evil.invalid"/></svg>`,
		`<style>@import "https://evil.invalid"; @font-face {font-family:bad;src:url(https://evil.invalid)} p{background:url(//evil.invalid);color:red}</style><p style="background:URL(https://evil.invalid); color:blue">safe</p>`,
		`<style>p{background:u\72l(https://evil.invalid);background-image:image-set("https://evil.invalid" 1x);color:red}</style>`,
		`<svg><a xlink:href="javascript:alert(1)"><text>safe</text></a><set attributeName="href" to="https://evil.invalid"/></svg>`,
		`<math><mtext><table><mglyph><style><!--</style><img title="--><img src=https://evil.invalid onerror=alert(1)>">`,
	} {
		t.Run(attack, func(t *testing.T) {
			body := string(sanitizeHTML([]byte(attack)))
			for _, forbidden := range []string{"<script", "onclick=", "onerror=", "onload=", "<meta", "<base", "<iframe", "<object", "<embed", "<form", "<input", "<button", "<link", "<img", "<video", "foreignobject", "<animate", "<set", "evil.invalid", "javascript:", "@import", "@font-face"} {
				if strings.Contains(strings.ToLower(body), forbidden) {
					t.Errorf("retained %q: %s", forbidden, body)
				}
			}
		})
	}
}

func TestHTMLFragmentsAndResponsiveCSS(t *testing.T) {
	body := string(sanitizeHTML([]byte(`<style>@media (max-width:600px){.a{display:grid;grid-template-columns:repeat(2,1fr)}}</style><svg viewBox="0 0 20 20"><defs><linearGradient id="paint"><stop offset="0" stop-color="blue"/></linearGradient></defs><rect fill="url(#paint)"/><use href="#shape"/></svg>`)))
	for _, want := range []string{"@media", "repeat(2,1fr)", "lineargradient", `fill="url(#paint)"`, `href="#shape"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s: %s", want, body)
		}
	}
}

func TestHTMLReadKeepsByteAndUTF8Guards(t *testing.T) {
	root := t.TempDir()
	for _, row := range []struct {
		name, body string
		want       error
	}{
		{"invalid.html", string([]byte{0xff}), NotFound},
		{"large.html", strings.Repeat("x", MaximumBytes+1), TooLarge},
		// Entity serialization grows this file past the existing output cap.
		{"expanded.html", strings.Repeat("& ", MaximumBytes/2), TooLarge},
	} {
		if err := os.WriteFile(filepath.Join(root, row.name), []byte(row.body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := Read(root, row.name); err != row.want {
			t.Errorf("%s: %v; want %v", row.name, err, row.want)
		}
	}
}

func TestHTMLKeepsReportColorVariablesWithoutResourceValues(t *testing.T) {
	body := string(sanitizeHTML([]byte(`<style>:root{--ink:#ddeeff;--remote:url(https://evil.invalid)}p{color:var(--ink,#fff)}</style><p>chart</p>`)))
	for _, want := range []string{"--ink: #ddeeff", "var(--ink,#fff)"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "evil.invalid") {
		t.Fatalf("resource variable survived: %s", body)
	}
}
