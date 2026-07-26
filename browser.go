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
	dir      string
	entries  []DirEntry
	cursor   int
	scroll   int
	selected []string
}

func NewBrowser() *Browser {
	return &Browser{
		dir:      ".",
		entries:  nil,
		cursor:   0,
		scroll:   0,
		selected: nil,
	}
}

func (b *Browser) SelectedFiles() []string {
	return b.selected
}

func (b *Browser) Run() ([]string, error) {
	b.selected = nil
	err := b.readDir(".")
	if err != nil {
		return nil, err
	}
	return b.selected, nil
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
	b.cursor = 0
	b.scroll = 0

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
			if ext == ".mp3" || ext == ".m3u" {
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

func (b *Browser) Cursor() int {
	return b.cursor
}

func (b *Browser) Scroll() int {
	return b.scroll
}

func (b *Browser) CursorUp() {
	if b.cursor > 0 {
		b.cursor--
	}
	if b.cursor < b.scroll {
		b.scroll = b.cursor
	}
}

func (b *Browser) CursorDown() {
	if b.cursor < len(b.entries)-1 {
		b.cursor++
	}
	if b.cursor >= b.scroll+maxDisplayEntries() {
		b.scroll = b.cursor - maxDisplayEntries() + 1
	}
}

func (b *Browser) Enter() (bool, error) {
	if b.cursor < 0 || b.cursor >= len(b.entries) {
		return false, nil
	}

	entry := b.entries[b.cursor]
	if entry.IsDir {
		files, err := resolvePath(entry.Path)
		if err != nil {
			return false, err
		}
		b.selected = files
		return true, nil
	}

	ext := strings.ToLower(filepath.Ext(entry.Path))
	if ext == ".m3u" {
		files, err := parseM3U(entry.Path)
		if err != nil {
			return false, err
		}
		b.selected = files
	} else {
		b.selected = []string{entry.Path}
	}
	return true, nil
}

func maxDisplayEntries() int {
	return 20
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

		if filepath.IsAbs(line) {
			files = append(files, line)
		} else {
			files = append(files, filepath.Join(baseDir, line))
		}
	}

	return files, nil
}
