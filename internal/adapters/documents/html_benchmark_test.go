package documents

import (
	"os"
	"path/filepath"
	"testing"
)

const staticHTMLFixture = `<style>.chart { color: #123; display: grid; }</style><main class="chart" style="padding: 8px"><h1>Report</h1><svg viewBox="0 0 100 40"><rect width="80" height="20" fill="blue"/></svg><table><tr><th>Time</th><td>42</td></tr></table><details><summary>More</summary>Evidence</details></main>`

func BenchmarkDocumentRead(b *testing.B) {
	root := b.TempDir()
	for _, row := range []struct{ name, body string }{{"report.html", staticHTMLFixture}, {"report.md", "# Report\n\nTime: 42\n"}} {
		if err := os.WriteFile(filepath.Join(root, row.name), []byte(row.body), 0600); err != nil {
			b.Fatal(err)
		}
		b.Run(row.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _, body, err := Read(root, row.name)
				if err != nil && err != NotFound {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(body)), "response-bytes")
			}
		})
	}
}
