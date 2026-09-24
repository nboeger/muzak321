package main

import (
	"embed"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
)

//go:embed defaultcovers/*.jpg
var defaultCoverFS embed.FS

// fallbackCoversDir returns the directory a user can drop, remove, or swap
// fallback cover art images in: $XDG_CONFIG_HOME/muzak321/covers (falling
// back to ~/.config per os.UserConfigDir).
func fallbackCoversDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "muzak321", "covers"), nil
}

// installDefaultCovers writes the bundled default cover art into
// fallbackCoversDir the first time it's needed. If the directory already
// exists, it is left untouched, so a user's own additions or deletions
// survive every subsequent run.
func installDefaultCovers() error {
	dir, err := fallbackCoversDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	entries, err := fs.ReadDir(defaultCoverFS, "defaultcovers")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		data, err := fs.ReadFile(defaultCoverFS, "defaultcovers/"+e.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// randomFallbackCover picks a random image from fallbackCoversDir for
// tracks with no embedded cover art of their own. It returns (nil, "") if
// the directory doesn't exist or is empty, so callers can treat it exactly
// like a track with no art at all.
func randomFallbackCover() ([]byte, string) {
	dir, err := fallbackCoversDir()
	if err != nil {
		return nil, ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, ""
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return nil, ""
	}
	name := files[rand.Intn(len(files))]
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, ""
	}
	return data, "image/jpeg"
}

// coverArtOrFallback returns data/mime unchanged when a track has its own
// embedded art; otherwise it returns a random image from
// fallbackCoversDir (or (nil, "") if none are installed).
func coverArtOrFallback(data []byte, mime string) ([]byte, string) {
	if len(data) > 0 {
		return data, mime
	}
	return randomFallbackCover()
}
