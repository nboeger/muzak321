package main

import (
	"os"
	"strings"

	"github.com/dhowden/tag"
)

// trackTitleArtist returns the title and artist from a file's tags.
func trackTitleArtist(path string) (title, artist string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return "", ""
	}
	return m.Title(), m.Artist()
}

// trackDisplayName returns "Title/Artist" for the audio file when the metadata
// is present, otherwise it falls back to the file name.
func trackDisplayName(path string) string {
	title, artist := trackTitleArtist(path)
	if title == "" {
		return cleanFileName(path)
	}

	var b strings.Builder
	b.WriteString(title)
	if artist != "" {
		b.WriteString("/")
		b.WriteString(artist)
	}
	return b.String()
}
