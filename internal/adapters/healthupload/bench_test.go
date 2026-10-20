package healthupload

import (
	"context"
	"go/build"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/healthjournal"
)

// BenchmarkAssembleAndEncode is the CPU and allocation cost of turning one
// 200-entry store page into one batch body: the work an active cycle adds
// on top of the store's own page read.
func BenchmarkAssembleAndEncode(b *testing.B) {
	t := &testing.T{}
	f := newFixture(t, b.TempDir(), "boot-bench")
	f.add(199)
	page, err := f.store.Read(healthjournal.Query{Limit: 200})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	var size int
	for i := 0; i < b.N; i++ {
		s := assemble(page, "")
		body, err := encode(wireBatch{SchemaVersion: WireSchemaVersion, BatchID: s.batchID(), ProducerInstance: ProducerInstance,
			BootID: s.boot, BuildCommit: s.build, Events: s.events})
		if err != nil {
			b.Fatal(err)
		}
		size = len(body)
		if i == 0 {
			b.ReportMetric(float64(len(s.events)), "events/batch")
		}
	}
	b.ReportMetric(float64(size), "body_bytes")
	f.close()
}

// BenchmarkIdleStep is one cycle with nothing new to send: a local identity
// read and one store page read, no request.
func BenchmarkIdleStep(b *testing.B) {
	t := &testing.T{}
	dir := b.TempDir()
	f := newFixture(t, dir, "boot-idle")
	f.add(10)
	c := newCloud()
	h := newHarness(t, f, c, filepath.Join(dir, "upload"))
	h.u.Step(context.Background())
	n := c.count()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.u.Step(context.Background())
	}
	b.StopTimer()
	if c.count() != n {
		b.Fatalf("an idle cycle made %d requests", c.count()-n)
	}
	f.close()
}

// The uploader must not share anything with the Cloud line it reports on:
// no package of the relay socket, its writer lock, its spool or its lanes is
// in its import graph, so a stuck line cannot hold an upload.
func TestTheUploaderDoesNotDependOnTheCloudLine(t *testing.T) {
	forbidden := []string{
		"github.com/sainteye/clawdline/internal/adapters/cloud",
		"github.com/sainteye/clawdline/internal/transport/cloud",
		"github.com/sainteye/clawdline/internal/domain/cloud",
	}
	seen := map[string]bool{}
	var walk func(path string)
	walk = func(path string) {
		if seen[path] || !strings.HasPrefix(path, "github.com/sainteye/clawdline/") {
			return
		}
		seen[path] = true
		for _, f := range forbidden {
			if path == f {
				t.Errorf("the uploader's import graph reaches %s", path)
			}
		}
		pkg, err := build.Default.Import(path, ".", 0)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, imp := range pkg.Imports {
			walk(imp)
		}
	}
	walk("github.com/sainteye/clawdline/internal/adapters/healthupload")
	if len(seen) < 3 {
		t.Fatalf("the walk saw %d packages; it is not reading the tree", len(seen))
	}
}
