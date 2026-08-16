package main

import (
	"reflect"
	"testing"
)

// TestNotifyArgs — exact argv shape: summary "muzak321", body
// "Title — Artist" or the file name when there are no tags.
func TestNotifyArgs(t *testing.T) {
	tests := []struct {
		name                string
		title, artist, file string
		want                []string
	}{
		{"title and artist", "Song", "Artist", "/music/song.mp3", []string{"muzak321", "Song — Artist"}},
		{"title only", "Song", "", "/music/song.mp3", []string{"muzak321", "Song"}},
		{"no tags", "", "", "/music/song.mp3", []string{"muzak321", "/music/song.mp3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := notifyArgs(tt.title, tt.artist, tt.file)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("notifyArgs(%q, %q, %q) = %v, want %v", tt.title, tt.artist, tt.file, got, tt.want)
			}
		})
	}
}

// TestNotifyDisabled — MUZAK321_NO_NOTIFY=1 disables the notifier (silent).
func TestNotifyDisabled(t *testing.T) {
	t.Setenv("MUZAK321_NO_NOTIFY", "1")
	// Must not panic or spawn anything; with the env set notify returns before
	// any exec, so this is trivially safe.
	notify("Song", "Artist", "/music/song.mp3")
}

// TestTrackTitleArtist — untagged/nonexistent files yield empty tags, not
// an error or panic.
func TestTrackTitleArtist(t *testing.T) {
	if title, artist := trackTitleArtist("/nonexistent/file.mp3"); title != "" || artist != "" {
		t.Errorf("trackTitleArtist(missing) = %q/%q, want empty", title, artist)
	}
}
