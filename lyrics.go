package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LyricLine is one synced lyric: a timestamp and its text.
type LyricLine struct {
	At   time.Duration
	Text string
}

var lrcTimestamp = regexp.MustCompile(`\[(\d{1,3}):(\d{1,2})(?:\.(\d{1,3}))?\]`)

// parseLRC parses timestamped [mm:ss.xx] / [mm:ss] lines, ignoring metadata
// tags ([ti:], [ar:], [offset:], ...). Multiple timestamps on one line
// produce one LyricLine each. Malformed lines are skipped.
func parseLRC(data []byte) []LyricLine {
	var lines []LyricLine
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		matches := lrcTimestamp.FindAllStringSubmatchIndex(line, -1)
		if len(matches) == 0 {
			continue
		}
		// Text is everything after the last timestamp.
		text := strings.TrimSpace(line[matches[len(matches)-1][1]:])
		for _, m := range matches {
			tag := line[m[0]:m[1]]
			at, ok := parseLRCTime(tag)
			if !ok {
				continue
			}
			lines = append(lines, LyricLine{At: at, Text: text})
		}
	}
	return lines
}

// parseLRCTime parses a "[mm:ss.xx]" / "[mm:ss]" / "[mm:ss.x]" tag.
func parseLRCTime(tag string) (time.Duration, bool) {
	inner := strings.TrimSuffix(strings.TrimPrefix(tag, "["), "]")
	parts := strings.SplitN(inner, ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	min, err := strconv.Atoi(parts[0])
	if err != nil || min < 0 {
		return 0, false
	}
	secStr := parts[1]
	sec := 0
	frac := 0
	if dot := strings.IndexByte(secStr, '.'); dot >= 0 {
		s, err := strconv.Atoi(secStr[:dot])
		if err != nil || s < 0 {
			return 0, false
		}
		sec = s
		digits := secStr[dot+1:]
		f, err := strconv.Atoi(digits)
		if err != nil || f < 0 {
			return 0, false
		}
		switch len(digits) {
		case 1:
			frac = f * 100
		case 2:
			frac = f * 10
		default: // 3+ digits: truncate to ms
			for len(digits) > 3 {
				f /= 10
				digits = digits[:len(digits)-1]
			}
			frac = f
		}
	} else {
		s, err := strconv.Atoi(secStr)
		if err != nil || s < 0 {
			return 0, false
		}
		sec = s
	}
	return time.Duration(min)*time.Minute + time.Duration(sec)*time.Second + time.Duration(frac)*time.Millisecond, true
}

// loadLRC returns parsed lyrics for a track path, or an error when no sibling
// <basename>.lrc exists.
func loadLRC(trackPath string) ([]LyricLine, error) {
	lrcPath := strings.TrimSuffix(trackPath, filepath.Ext(trackPath)) + ".lrc"
	data, err := os.ReadFile(lrcPath)
	if err != nil {
		return nil, err
	}
	return parseLRC(data), nil
}

// currentLyric returns the index of the last line with At <= pos, or -1.
func currentLyric(lines []LyricLine, pos time.Duration) int {
	idx := -1
	for i, l := range lines {
		if l.At <= pos {
			idx = i
		} else {
			break
		}
	}
	return idx
}
