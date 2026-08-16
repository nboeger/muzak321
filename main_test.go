package main

import (
	"reflect"
	"testing"
)

// TestPlayArgs — positional arguments and the -f flag both select play
// mode; the flag value always comes first so the playlist order is stable.
func TestPlayArgs(t *testing.T) {
	tests := []struct {
		name     string
		fileArg  string
		args     []string
		expected []string
	}{
		{"no input", "", nil, []string{}},
		{"flag only", "song.mp3", nil, []string{"song.mp3"}},
		{"positional only", "", []string{"song.mp3"}, []string{"song.mp3"}},
		{"multiple positional", "", []string{"a.mp3", "b.mp3", "mix.m3u"}, []string{"a.mp3", "b.mp3", "mix.m3u"}},
		{"flag plus positional", "first.mp3", []string{"second.mp3"}, []string{"first.mp3", "second.mp3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := playArgs(tt.fileArg, tt.args)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("playArgs(%q, %v) = %v, want %v", tt.fileArg, tt.args, got, tt.expected)
			}
		})
	}
}
