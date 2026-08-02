package main

import (
	"os"
	"strings"

	"github.com/dhowden/tag"
)

// trackDisplayName returns "Title/Artist" for the audio file when the metadata
// is present, otherwise it falls back to the file name.
func trackDisplayName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return cleanFileName(path)
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return cleanFileName(path)
	}

	title := m.Title()
	if title == "" {
		return cleanFileName(path)
	}

	var b strings.Builder
	b.WriteString(title)
	if artist := m.Artist(); artist != "" {
		b.WriteString("/")
		b.WriteString(artist)
	}
	return b.String()
}
