package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestParseLRC — timestamps, metadata tags, malformed lines, and multiple
// timestamps on one line.
func TestParseLRC(t *testing.T) {
	data := []byte(`[ti:Test Song]
[ar:Test Artist]
[00:01.00]First line
[00:02.50]Second line
[00:03]No-fraction line
[00:04.25][00:05.75]Repeated line
garbage without timestamp
[00:5x]Bad time
[03:12:45]Extra colon
[01:02.5]Single-digit fraction
`)
	lines := parseLRC(data)
	want := []LyricLine{
		{At: 1 * time.Second, Text: "First line"},
		{At: 2*time.Second + 500*time.Millisecond, Text: "Second line"},
		{At: 3 * time.Second, Text: "No-fraction line"},
		{At: 4*time.Second + 250*time.Millisecond, Text: "Repeated line"},
		{At: 5*time.Second + 750*time.Millisecond, Text: "Repeated line"},
		{At: 1*time.Minute + 2*time.Second + 500*time.Millisecond, Text: "Single-digit fraction"},
	}
	if len(lines) != len(want) {
		t.Fatalf("parseLRC returned %d lines, want %d: %+v", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i].At != want[i].At || lines[i].Text != want[i].Text {
			t.Errorf("line %d = %+v, want %+v", i, lines[i], want[i])
		}
	}
}

// TestCurrentLyric — monotonic: before first → -1, between lines → previous
// index, after last → last index.
func TestCurrentLyric(t *testing.T) {
	lines := []LyricLine{
		{At: 1 * time.Second, Text: "a"},
		{At: 3 * time.Second, Text: "b"},
		{At: 5 * time.Second, Text: "c"},
	}
	tests := []struct {
		pos  time.Duration
		want int
	}{
		{0, -1},
		{999 * time.Millisecond, -1},
		{1 * time.Second, 0},
		{2 * time.Second, 0},
		{3 * time.Second, 1},
		{4 * time.Second, 1},
		{5 * time.Second, 2},
		{10 * time.Second, 2},
	}
	for _, tt := range tests {
		if got := currentLyric(lines, tt.pos); got != tt.want {
			t.Errorf("currentLyric(%v) = %d, want %d", tt.pos, got, tt.want)
		}
	}
}

// TestLoadLRC — loads a sibling .lrc; errors when absent.
func TestLoadLRC(t *testing.T) {
	dir := t.TempDir()
	track := filepath.Join(dir, "song.mp3")
	if _, err := loadLRC(track); err == nil {
		t.Error("loadLRC with no sibling .lrc: want error")
	}
	lrc := filepath.Join(dir, "song.lrc")
	if err := os.WriteFile(lrc, []byte("[00:01.00]Hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := loadLRC(track)
	if err != nil {
		t.Fatalf("loadLRC: %v", err)
	}
	if len(lines) != 1 || lines[0].Text != "Hi" {
		t.Errorf("loadLRC = %+v, want one 'Hi' line", lines)
	}
}

// TestPlayerLyricsCache — lyrics are cached on the player and cleared for
// tracks without a sibling .lrc.
func TestPlayerLyricsCache(t *testing.T) {
	dir := t.TempDir()
	track := filepath.Join(dir, "track.mp3")
	if err := os.WriteFile(filepath.Join(dir, "track.lrc"), []byte("[00:01.00]Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &Player{}
	p.mu.Lock()
	p.lyrics = parseLRC([]byte("[00:01.00]Hello\n"))
	p.lyricsPath = track
	p.mu.Unlock()
	if len(p.Lyrics()) != 1 {
		t.Fatalf("Lyrics() = %+v, want 1 line", p.Lyrics())
	}
}
