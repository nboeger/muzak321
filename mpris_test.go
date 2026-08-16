package main

import (
	"path/filepath"
	"testing"
)

// TestMPRISPlaybackStatus — state maps to the MPRIS PlaybackStatus contract.
func TestMPRISPlaybackStatus(t *testing.T) {
	tests := []struct {
		state PlayerState
		want  string
	}{
		{StatePlaying, "Playing"},
		{StatePaused, "Paused"},
		{StateMuted, "Paused"},
		{StateStopped, "Stopped"},
	}
	for _, tt := range tests {
		m := &mpris{p: &Player{state: tt.state}}
		if got := m.playbackStatus(); got != tt.want {
			t.Errorf("playbackStatus(%v) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// TestMPRISMetadata — metadata carries xesam:url + title (file name when the
// file has no tags); no artist key when absent.
func TestMPRISMetadata(t *testing.T) {
	dir := t.TempDir()
	track := filepath.Join(dir, "untagged.wav")
	writeToneWAV(t, track) // WAV has no tags for dhowden/tag

	m := &mpris{p: &Player{}}
	p := &Player{}
	p.mu.Lock()
	p.files = []string{track}
	p.order = []int{0}
	p.currentIdx = 0
	p.mu.Unlock()
	m = &mpris{p: p}

	md := m.metadata()
	if _, ok := md["xesam:url"]; !ok {
		t.Error("metadata missing xesam:url")
	}
	if _, ok := md["xesam:title"]; !ok {
		t.Error("metadata missing xesam:title")
	}
	if _, ok := md["xesam:artist"]; ok {
		t.Error("untagged file should have no xesam:artist")
	}
}

// TestMPRISGetAll — GetAll enumerates the documented properties without error.
func TestMPRISGetAll(t *testing.T) {
	m := &mpris{p: &Player{state: StateStopped}}
	root, err := m.GetAll("", mprisIface)
	if err != nil {
		t.Fatalf("GetAll(root): %v", err)
	}
	if _, ok := root["Identity"]; !ok {
		t.Error("root GetAll missing Identity")
	}
	player, err := m.GetAll("", mprisPlayer)
	if err != nil {
		t.Fatalf("GetAll(player): %v", err)
	}
	for _, p := range []string{"PlaybackStatus", "CanGoNext", "CanGoPrevious", "CanPlay", "CanPause", "CanSeek", "Metadata", "Position"} {
		if _, ok := player[p]; !ok {
			t.Errorf("player GetAll missing %s", p)
		}
	}
}
