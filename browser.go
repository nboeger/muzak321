package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DirEntry struct {
	Name  string
	IsDir bool
	Path  string
}

type Browser struct {
	dir     string
	entries []DirEntry
}

func NewBrowser() *Browser {
	return &Browser{dir: "."}
}

func (b *Browser) readDir(dirPath string) error {
	abs, err := filepath.Abs(dirPath)
	if err != nil {
		return err
	}

	f, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer f.Close()

	names, err := f.Readdirnames(-1)
	if err != nil {
		return err
	}

	b.dir = abs
	b.entries = nil

	var dirs, files []DirEntry

	parent := DirEntry{
		Name:  "..",
		IsDir: true,
		Path:  filepath.Dir(abs),
	}
	if abs != "/" {
		dirs = append(dirs, parent)
	}

	for _, name := range names {
		fullPath := filepath.Join(abs, name)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		entry := DirEntry{
			Name:  name,
			Path:  fullPath,
			IsDir: info.IsDir(),
		}

		if info.IsDir() {
			if !strings.HasPrefix(name, ".") {
				dirs = append(dirs, entry)
			}
		} else {
			ext := strings.ToLower(filepath.Ext(name))
			if isAudioFile(ext) || ext == ".m3u" {
				files = append(files, entry)
			}
		}
	}

	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name)
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	b.entries = append(dirs, files...)
	return nil
}

func (b *Browser) Navigate(dir string) error {
	return b.readDir(dir)
}

func (b *Browser) Entries() []DirEntry {
	return b.entries
}

func (b *Browser) Dir() string {
	return b.dir
}

// DirAudioFiles returns every playable file directly in the current directory:
// audio files plus any playlists (.m3u) found there, expanded to their tracks.
func (b *Browser) DirAudioFiles() ([]string, error) {
	f, err := os.Open(b.dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	var result []string
	for _, name := range names {
		fullPath := filepath.Join(b.dir, name)
		info, err := os.Stat(fullPath)
		if err != nil || info.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if isAudioFile(ext) {
			result = append(result, fullPath)
		} else if ext == ".m3u" || ext == ".pls" {
			files, _, err := parsePlaylist(fullPath)
			if err != nil {
				continue
			}
			result = append(result, files...)
		}
	}
	return result, nil
}

func isAudioFile(ext string) bool {
	switch ext {
	case ".mp3", ".flac", ".ogg", ".wav":
		return true
	default:
		return false
	}
}

// parsePlaylist expands a playlist file, reporting ok=false for non-playlists.
func parsePlaylist(path string) (files []string, ok bool, err error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m3u":
		f, e := parseM3U(path)
		return f, true, e
	case ".pls":
		f, e := parsePLS(path)
		return f, true, e
	}
	return nil, false, nil
}

func parseM3U(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Strip UTF-8 BOM if present
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}

	baseDir := filepath.Dir(path)
	var files []string
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip surrounding quotes (some M3U generators quote paths with spaces)
		if len(line) > 1 {
			if line[0] == '"' && line[len(line)-1] == '"' {
				line = line[1 : len(line)-1]
			} else if line[0] == '\'' && line[len(line)-1] == '\'' {
				line = line[1 : len(line)-1]
			}
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if isStreamURL(line) || filepath.IsAbs(line) {
			files = append(files, line)
		} else {
			files = append(files, filepath.Join(baseDir, line))
		}
	}

	return files, nil
}
