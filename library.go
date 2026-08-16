package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Library storage constants.
const (
	HistoryMax   = 200
	historyFile  = "history.log"
	playlistFile = "last.m3u"
)

// dataDir returns the library directory: $MUZAK321_DATA_DIR when set,
// otherwise ~/.muzak321.
func dataDir() string {
	if d := os.Getenv("MUZAK321_DATA_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".muzak321")
	}
	return filepath.Join(home, ".muzak321")
}

// ensureDataDir creates the data directory (0700) on first use.
func ensureDataDir() (string, error) {
	dir := dataDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// SavePlaylist writes a plain m3u (#EXTM3U header, one absolute path per
// line) that round-trips through parseM3U.
func SavePlaylist(path string, files []string) error {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	for _, f := range files {
		b.WriteString(f)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// readHistory returns history entries oldest-first: [ts, path] pairs.
func readHistory(path string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries [][]string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) == 2 {
			entries = append(entries, parts)
		}
	}
	return entries, nil
}

// writeHistory rewrites the history file oldest-first.
func writeHistory(path string, entries [][]string) error {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(e[0])
		b.WriteByte('\t')
		b.WriteString(e[1])
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// AppendHistory appends unix-ts\tpath to history.log, deduping consecutive
// repeats (bumping the timestamp instead) and trimming to HistoryMax entries.
func AppendHistory(trackPath string) error {
	dir, err := ensureDataDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, historyFile)
	entries, _ := readHistory(path)
	now := strconv.FormatInt(time.Now().Unix(), 10)
	if len(entries) > 0 && entries[len(entries)-1][1] == trackPath {
		entries[len(entries)-1][0] = now // consecutive repeat: refresh recency
	} else {
		entries = append(entries, []string{now, trackPath})
	}
	if len(entries) > HistoryMax {
		entries = entries[len(entries)-HistoryMax:]
	}
	return writeHistory(path, entries)
}

// LoadHistory returns [ts, path] pairs, most recent first.
func LoadHistory() [][]string {
	entries, err := readHistory(filepath.Join(dataDir(), historyFile))
	if err != nil {
		return nil
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries
}

// formatHistoryTime renders a unix timestamp as "01-02 15:04".
func formatHistoryTime(ts string) string {
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "?"
	}
	return time.Unix(sec, 0).Format("01-02 15:04")
}
