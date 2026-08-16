package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestSavePlaylistRoundTrip — SavePlaylist output must round-trip through
// parseM3U, returning the same file list.
func TestSavePlaylistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		filepath.Join(dir, "a.mp3"),
		filepath.Join(dir, "b.flac"),
		filepath.Join(dir, "c.ogg"),
	}
	pl := filepath.Join(dir, "last.m3u")
	if err := SavePlaylist(pl, files); err != nil {
		t.Fatal(err)
	}
	got, err := parseM3U(pl)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(files) {
		t.Fatalf("round-trip length = %d, want %d", len(got), len(files))
	}
	for i := range files {
		if got[i] != files[i] {
			t.Errorf("round-trip[%d] = %q, want %q", i, got[i], files[i])
		}
	}
}

// TestAppendHistory — appends, dedupes consecutive repeats, and trims to 200.
func TestAppendHistory(t *testing.T) {
	oldDataDir := os.Getenv("MUZAK321_DATA_DIR")
	dir := t.TempDir()
	t.Setenv("MUZAK321_DATA_DIR", dir)
	defer os.Setenv("MUZAK321_DATA_DIR", oldDataDir)

	for i := 0; i < HistoryMax+10; i++ {
		track := filepath.Join(dir, "tracks", fmt.Sprintf("track-%03d.mp3", i))
		if err := AppendHistory(track); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := readHistory(filepath.Join(dir, historyFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != HistoryMax {
		t.Fatalf("history length = %d, want %d", len(entries), HistoryMax)
	}

	// Consecutive repeat: same path appends nothing new, refreshes the ts.
	path := filepath.Join(dir, "tracks", "unique.mp3")
	if err := AppendHistory(path); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistory(path); err != nil {
		t.Fatal(err)
	}
	entries, _ = readHistory(filepath.Join(dir, historyFile))
	if len(entries) != HistoryMax {
		t.Fatalf("history length after dedupe = %d, want %d", len(entries), HistoryMax)
	}
	if entries[len(entries)-1][1] != path {
		t.Errorf("last entry = %q, want %q", entries[len(entries)-1][1], path)
	}
	// Distinct path appends a new entry.
	if err := AppendHistory(filepath.Join(dir, "tracks", "other.mp3")); err != nil {
		t.Fatal(err)
	}
	entries, _ = readHistory(filepath.Join(dir, historyFile))
	if entries[len(entries)-1][1] != filepath.Join(dir, "tracks", "other.mp3") {
		t.Errorf("distinct append failed: last = %q", entries[len(entries)-1][1])
	}
}

// TestLoadHistoryMostRecentFirst — LoadHistory returns most-recent-first.
func TestLoadHistoryMostRecentFirst(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUZAK321_DATA_DIR", dir)
	path := filepath.Join(dir, historyFile)
	if err := writeHistory(path, [][]string{
		{"100", "/old.mp3"},
		{"200", "/new.mp3"},
	}); err != nil {
		t.Fatal(err)
	}
	entries := LoadHistory()
	if len(entries) != 2 || entries[0][1] != "/new.mp3" || entries[1][1] != "/old.mp3" {
		t.Errorf("LoadHistory = %v, want most-recent-first", entries)
	}
}
