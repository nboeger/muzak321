package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallDefaultCoversCreatesFiles(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := installDefaultCovers(); err != nil {
		t.Fatalf("installDefaultCovers: %v", err)
	}

	dir, err := fallbackCoversDir()
	if err != nil {
		t.Fatalf("fallbackCoversDir: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected default cover files to be installed, dir is empty")
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatalf("Info(%s): %v", e.Name(), err)
		}
		if info.Size() == 0 {
			t.Errorf("installed cover %s is empty", e.Name())
		}
	}
}

func TestInstallDefaultCoversDoesNotOverwriteExisting(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir, err := fallbackCoversDir()
	if err != nil {
		t.Fatalf("fallbackCoversDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my-own-cover.jpg"), []byte("user art"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installDefaultCovers(); err != nil {
		t.Fatalf("installDefaultCovers: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	if len(entries) != 1 || entries[0].Name() != "my-own-cover.jpg" {
		t.Errorf("expected only the user's own file to remain, got %v", entries)
	}
}

func TestRandomFallbackCoverReturnsFileFromDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir, err := fallbackCoversDir()
	if err != nil {
		t.Fatalf("fallbackCoversDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{
		"a.jpg": []byte("cover a bytes"),
		"b.jpg": []byte("cover b bytes"),
	}
	for name, data := range want {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		data, mime := randomFallbackCover()
		if mime != "image/jpeg" {
			t.Errorf("mime = %q, want %q", mime, "image/jpeg")
		}
		matched := false
		for _, want := range want {
			if string(data) == string(want) {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("randomFallbackCover() returned unexpected data %q", data)
		}
		seen[string(data)] = true
	}
	if len(seen) < 2 {
		t.Errorf("randomFallbackCover() never varied across 20 calls; want randomness across the 2 files")
	}
}

func TestRandomFallbackCoverEmptyDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	dir, err := fallbackCoversDir()
	if err != nil {
		t.Fatalf("fallbackCoversDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	data, mime := randomFallbackCover()
	if data != nil || mime != "" {
		t.Errorf("randomFallbackCover() on empty dir = (%v, %q), want (nil, \"\")", data, mime)
	}
}

func TestCoverArtOrFallbackPassesThroughRealArt(t *testing.T) {
	data, mime := coverArtOrFallback([]byte("real art"), "image/png")
	if string(data) != "real art" || mime != "image/png" {
		t.Errorf("coverArtOrFallback with real art = (%q, %q), want unchanged", data, mime)
	}
}

func TestCoverArtOrFallbackUsesFallbackWhenEmpty(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err := fallbackCoversDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.jpg"), []byte("fallback bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	data, mime := coverArtOrFallback(nil, "")
	if string(data) != "fallback bytes" || mime != "image/jpeg" {
		t.Errorf("coverArtOrFallback with no art = (%q, %q), want fallback", data, mime)
	}
}

func TestRandomFallbackCoverMissingDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	data, mime := randomFallbackCover()
	if data != nil || mime != "" {
		t.Errorf("randomFallbackCover() with no covers dir = (%v, %q), want (nil, \"\")", data, mime)
	}
}
