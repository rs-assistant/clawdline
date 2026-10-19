package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestUnpackRejectsOverflowingDeclaredSizeBeforeReadingPayload(t *testing.T) {
	var body bytes.Buffer
	gz := gzip.NewWriter(&body)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "small", Mode: 0o600, Size: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "huge", Mode: 0o600, Size: math.MaxInt64}); err != nil {
		t.Fatal(err)
	}
	// Deliberately omit the huge payload. The size bound must reject its
	// header, rather than start copying and then discover truncation.
	_ = tw.Close()
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	archive, dir := filepath.Join(base, "release.tar.gz"), filepath.Join(base, "unpacked")
	if err := os.WriteFile(archive, body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := unpack(archive, dir); errCode(err) != CodeArchiveUnsafe {
		t.Fatalf("overflowing size reached payload reader: %v", err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("refused archive left directory: %v", err)
	}
}

func TestUnpackRejectsSymlinkChainsThatEscape(t *testing.T) {
	for _, forward := range []bool{false, true} {
		t.Run(map[bool]string{false: "backward", true: "forward"}[forward], func(t *testing.T) {
			base := t.TempDir()
			outside := filepath.Join(base, "outside")
			if err := os.WriteFile(outside, []byte("outside release"), 0o600); err != nil {
				t.Fatal(err)
			}
			links := []entry{{name: "alias", typ: tar.TypeSymlink, link: "."}, {name: "escape", typ: tar.TypeSymlink, link: "alias/../outside"}}
			if forward {
				links[0], links[1] = links[1], links[0]
			}
			archive := filepath.Join(base, "release.tar.gz")
			if err := os.WriteFile(archive, tarGz(t, links...), 0o600); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(base, "unpacked")
			if err := unpack(archive, dir); errCode(err) != CodeArchiveUnsafe {
				body, readErr := os.ReadFile(filepath.Join(dir, "escape"))
				t.Fatalf("escaping chain accepted: unpack=%v, outside read=%q (%v)", err, body, readErr)
			}
			if _, err := os.Lstat(dir); !os.IsNotExist(err) {
				t.Fatalf("refused archive left directory: %v", err)
			}
		})
	}
}

func TestUnpackKeepsInternalForwardAndDanglingSymlinks(t *testing.T) {
	base := t.TempDir()
	archive := filepath.Join(base, "release.tar.gz")
	entries := []entry{
		{name: "Versions/Current", typ: tar.TypeSymlink, link: "A"},
		{name: "binary", typ: tar.TypeSymlink, link: "Versions/Current/binary"},
		{name: "Versions/A/binary", body: "binary"},
		{name: "dangling", typ: tar.TypeSymlink, link: "missing"},
	}
	if err := os.WriteFile(archive, tarGz(t, entries...), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "unpacked")
	if err := unpack(archive, dir); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "binary")); err != nil || string(body) != "binary" {
		t.Fatalf("internal chain: %q %v", body, err)
	}
	if link, err := os.Readlink(filepath.Join(dir, "dangling")); err != nil || link != "missing" {
		t.Fatalf("dangling link: %q %v", link, err)
	}
}
