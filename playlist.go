package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// parsePLS reads a .pls playlist and returns the track list. Entries may be
// local paths (resolved relative to the playlist file) or remote http(s) URLs.
func parsePLS(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parsePLSLines(string(data), filepath.Dir(path))
}

func parsePLSLines(content, baseDir string) ([]string, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	entries := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if strings.HasPrefix(strings.ToLower(key), "file") {
			entries[strings.ToLower(key)] = val
		}
	}
	if len(entries) == 0 {
		return nil, os.ErrNotExist
	}

	var tracks []string
	for i := 1; ; i++ {
		k := "file" + strconv.Itoa(i)
		v, ok := entries[k]
		if !ok {
			break
		}
		v = strings.Trim(v, `"'`)
		if isStreamURL(v) {
			tracks = append(tracks, v)
		} else if baseDir != "" && !filepath.IsAbs(v) {
			if _, err := os.Stat(filepath.Join(baseDir, v)); err == nil {
				v = filepath.Join(baseDir, v)
			}
			tracks = append(tracks, v)
		} else {
			tracks = append(tracks, v)
		}
	}
	if len(tracks) == 0 {
		return nil, os.ErrNotExist
	}
	return tracks, nil
}
