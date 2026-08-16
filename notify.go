package main

import (
	"os"
	"os/exec"
)

// notifyArgs builds the notify-send argv for a track: summary "muzak321",
// body "Title — Artist" or the file name when there are no tags.
func notifyArgs(title, artist, file string) []string {
	body := file
	if title != "" {
		body = title
		if artist != "" {
			body = title + " — " + artist
		}
	}
	return []string{"muzak321", body}
}

// notify fires a desktop notification in a detached goroutine. All errors are
// swallowed (best-effort by design); MUZAK321_NO_NOTIFY=1 or a missing
// notify-send make it a silent no-op. Never blocks the audio path.
func notify(title, artist, file string) {
	if os.Getenv("MUZAK321_NO_NOTIFY") != "" {
		return
	}
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return
	}
	args := append([]string{"-a", "muzak321"}, notifyArgs(title, artist, file)...)
	go func() {
		exec.Command(path, args...).Start() // error intentionally ignored
	}()
}
