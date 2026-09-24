package main

import (
	"reflect"
	"testing"
)

// TestNormalizeTerm — some terminal emulators (kitty, in particular) are
// commonly misconfigured with a bare $TERM value that has no terminfo
// entry under that exact name (kitty ships its entry as "xterm-kitty").
// tcell falls back to shelling out to `infocmp`, which fails silently in
// confined environments (e.g. strict snaps) with no useful diagnostic.
// normalizeTerm remaps the known-bad values to the name tcell actually
// has built in, so the app works without requiring a $TERM workaround.
func TestNormalizeTerm(t *testing.T) {
	tests := []struct{ in, want string }{
		{"kitty", "xterm-kitty"},
		{"xterm-kitty", "xterm-kitty"}, // already correct: unchanged
		{"xterm-256color", "xterm-256color"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeTerm(tt.in); got != tt.want {
			t.Errorf("normalizeTerm(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

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
