package install

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type installArchiveEntry struct {
	name, link, body string
	kind             byte
}

func orderedInstallArchive(t *testing.T, entries []installArchiveEntry) string {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "archive.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Linkname: e.link, Typeflag: e.kind, Mode: 0o755}
		if e.kind == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, close := range []func() error{tw.Close, gz.Close, f.Close} {
		if err := close(); err != nil {
			t.Fatal(err)
		}
	}
	return f.Name()
}

func TestInstallExtractRejectsComposedLinksAndWrites(t *testing.T) {
	for _, forward := range []bool{false, true} {
		for _, write := range []bool{false, true} {
			t.Run(fmtInstallCase(forward, write), func(t *testing.T) {
				base := t.TempDir()
				outside := filepath.Join(base, "outside")
				if err := os.Mkdir(outside, 0o755); err != nil {
					t.Fatal(err)
				}
				entries := []installArchiveEntry{{name: "alias", link: ".", kind: tar.TypeSymlink}, {name: "escape", link: "alias/../outside", kind: tar.TypeSymlink}}
				if forward {
					entries[0], entries[1] = entries[1], entries[0]
				}
				if write {
					entries = append(entries, installArchiveEntry{name: "escape/payload", body: "escaped", kind: tar.TypeReg})
				}
				err := Extract(orderedInstallArchive(t, entries), filepath.Join(base, "tree"))
				if body, readErr := os.ReadFile(filepath.Join(outside, "payload")); readErr == nil {
					t.Errorf("outside payload was written: %q", body)
				}
				if err == nil {
					t.Error("accepted a link resolving outside the extraction root")
				}
			})
		}
	}
}

func fmtInstallCase(forward, write bool) string {
	order, action := "backward", "link"
	if forward {
		order = "forward"
	}
	if write {
		action = "write"
	}
	return order + "/" + action
}

func TestInstallExtractRejectsWritesThroughInternalLinks(t *testing.T) {
	archive := orderedInstallArchive(t, []installArchiveEntry{{name: "real", kind: tar.TypeDir}, {name: "alias", link: "real", kind: tar.TypeSymlink}, {name: "alias/payload", body: "x", kind: tar.TypeReg}})
	if err := Extract(archive, filepath.Join(t.TempDir(), "tree")); err == nil {
		t.Fatal("wrote through an internal symlink parent")
	}
}

func TestInstallVerifyTreeRequiresActualEntryTypesAndParents(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(fmtInstallCase(parent, false), func(t *testing.T) {
			base := t.TempDir()
			outside := filepath.Join(base, "outside")
			if err := os.Mkdir(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "payload"), []byte("signed"), 0o600); err != nil {
				t.Fatal(err)
			}
			tree := filepath.Join(base, "tree")
			if err := os.Mkdir(tree, 0o755); err != nil {
				t.Fatal(err)
			}
			name, target, local := "payload", "../outside/payload", "payload"
			if parent {
				name, target, local = "alias/payload", "../outside", "alias"
			}
			if err := os.Symlink(target, filepath.Join(tree, local)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			archive := orderedInstallArchive(t, []installArchiveEntry{{name: name, body: "signed", kind: tar.TypeReg}})
			var refusal *Refusal
			if err := VerifyTree(archive, tree); !errors.As(err, &refusal) || refusal.Code != CodeTreeMismatch {
				t.Fatalf("accepted external bytes as a signed regular file: %v", err)
			}
		})
	}
}

func TestInstallArchiveKeepsAppForwardAndDanglingLinks(t *testing.T) {
	archive := orderedInstallArchive(t, []installArchiveEntry{
		{name: "App.app/Versions/Current", link: "A", kind: tar.TypeSymlink},
		{name: "App.app/Resources", link: "Versions/Current/Resources", kind: tar.TypeSymlink},
		{name: "App.app/optional", link: "missing", kind: tar.TypeSymlink},
		{name: "App.app/Versions/A/Resources/data", body: "app", kind: tar.TypeReg},
	})
	tree := filepath.Join(t.TempDir(), "tree")
	if err := Extract(archive, tree); err != nil {
		t.Fatal(err)
	}
	if err := VerifyTree(archive, tree); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(tree, "App.app/Resources/data")); err != nil || string(body) != "app" {
		t.Fatalf("internal chain: %q, %v", body, err)
	}
}

func TestInstallArchiveRejectsDanglingEscapes(t *testing.T) {
	archive := orderedInstallArchive(t, []installArchiveEntry{
		{name: "escape", link: "alias/../outside/missing", kind: tar.TypeSymlink},
		{name: "alias", link: ".", kind: tar.TypeSymlink},
	})
	base := t.TempDir()
	tree := filepath.Join(base, "tree")
	t.Run("extract", func(t *testing.T) {
		out := filepath.Join(base, "extracted")
		if err := Extract(archive, out); err == nil {
			t.Error("accepted an external dangling target")
		}
		if _, err := os.Lstat(out); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("failed extraction left a partial tree: %v", err)
		}
	})
	// Model the shell-unpacked tree consumed by VerifyTree independently of
	// Extract, which must never produce this tree after the fix.
	if err := os.Mkdir(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"escape": "alias/../outside/missing", "alias": "."} {
		if err := os.Symlink(target, filepath.Join(tree, name)); err != nil {
			t.Fatal(err)
		}
	}
	var refusal *Refusal
	if err := VerifyTree(archive, tree); !errors.As(err, &refusal) || refusal.Code != CodeTreeMismatch {
		t.Fatalf("accepted an external dangling link during verification: %v", err)
	}
}

func TestInstallArchiveRejectsDirectoryAliasesAndSpecialEntries(t *testing.T) {
	for label, entry := range map[string]installArchiveEntry{
		"directory-alias": {name: "alias", kind: tar.TypeDir},
		"hardlink":        {name: "hard", link: "real", kind: tar.TypeLink},
		"fifo":            {name: "pipe", kind: tar.TypeFifo},
	} {
		t.Run(label, func(t *testing.T) {
			archive := orderedInstallArchive(t, []installArchiveEntry{
				{name: "real", kind: tar.TypeDir},
				{name: "alias", link: "real", kind: tar.TypeSymlink},
				entry,
			})
			base := t.TempDir()
			tree := filepath.Join(base, "extracted")
			if err := Extract(archive, tree); err == nil {
				t.Error("accepted an entry whose actual type differs")
			}
			tree = filepath.Join(base, "verify")
			if err := os.MkdirAll(filepath.Join(tree, "real"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("real", filepath.Join(tree, "alias")); err != nil {
				t.Fatal(err)
			}
			var refusal *Refusal
			if err := VerifyTree(archive, tree); !errors.As(err, &refusal) || refusal.Code != CodeTreeMismatch {
				t.Fatalf("verified an unsupported or mismatched type: %v", err)
			}
		})
	}
}

func TestInstallExtractPreservesAnExistingRoot(t *testing.T) {
	base := t.TempDir()
	tree := filepath.Join(base, "tree")
	if err := os.Mkdir(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(tree, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := orderedInstallArchive(t, []installArchiveEntry{{name: "file", body: "x", kind: tar.TypeReg}})
	if err := Extract(archive, tree); err == nil {
		t.Fatal("accepted an existing root")
	}
	if body, err := os.ReadFile(marker); err != nil || string(body) != "keep" {
		t.Fatalf("existing root changed: %q %v", body, err)
	}
}
