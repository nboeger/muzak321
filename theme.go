package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Themeable colors, exposed as package vars so loadTheme can override them
// at startup. Defaults reproduce the existing btop-style palette exactly.
var (
	colHeader   = mustColor("3a3a3a")
	colPaleText = mustColor("c0c0c0")
	colError    = mustColor("a86b6b")

	borderColorPlaylist = mustColor("6b9b9b")
	borderColorCoverArt = mustColor("a88bb5")
	borderColorSpectrum = mustColor("8fb08a")

	spectrumLow  = mustColor("5fb05f")
	spectrumMid  = mustColor("b0b05f")
	spectrumHigh = mustColor("b05f5f")

	colBodyBG = mustColor("000000")

	colPlaylistSelFG = mustColor("ffffff")
	colPlaylistSelBG = mustColor("000000")
	colBrowserSelFG  = mustColor("000000")
	colBrowserSelBG  = mustColor("ffffff")
)

// Text-tag colors: pre-built tview markup strings, rebuilt whenever their
// underlying hex changes (see themeSetters).
var (
	barFillBGHex = "2a2a2a"
	barFillFGHex = "6b9b8f"
	colBarFill   = buildBarFill()

	accentAmberHex = "c9b46b"
	colAmber       = "[#" + accentAmberHex + "]"

	accentTealHex = "6b9b9b"
	colTeal       = "[#" + accentTealHex + "]"
)

const colReset = "[-:-]"

func buildBarFill() string {
	return "[#" + barFillBGHex + ":" + barFillFGHex + "]"
}

// hexToColor parses a "#rrggbb" string into a tcell.Color.
func hexToColor(s string) (tcell.Color, error) {
	if len(s) != 7 || s[0] != '#' {
		return 0, fmt.Errorf("invalid color %q: want #rrggbb", s)
	}
	v, err := strconv.ParseInt(s[1:], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid color %q: %w", s, err)
	}
	r := int32((v >> 16) & 0xff)
	g := int32((v >> 8) & 0xff)
	b := int32(v & 0xff)
	return tcell.NewRGBColor(r, g, b), nil
}

// colorHex returns the 6-digit lowercase hex string for a tcell.Color, with
// no leading '#'.
func colorHex(c tcell.Color) string {
	r, g, b := c.RGB()
	return fmt.Sprintf("%02x%02x%02x", r, g, b)
}

// mustColor parses a 6-digit hex string (no leading '#') into a tcell.Color.
// It panics on invalid input; only used for the compile-time-correct
// default palette above.
func mustColor(hex string) tcell.Color {
	c, err := hexToColor("#" + hex)
	if err != nil {
		panic(err)
	}
	return c
}

// themeSetters maps each theme.conf key to the function that applies a
// validated 6-digit hex value (no leading '#') to the matching package
// var(s).
var themeSetters = map[string]func(hex string){
	"header_bg": func(hex string) { colHeader = mustColor(hex) },
	"header_fg": func(hex string) { colPaleText = mustColor(hex) },
	"error_bg":  func(hex string) { colError = mustColor(hex) },

	"bar_fill_bg": func(hex string) { barFillBGHex = hex; colBarFill = buildBarFill() },
	"bar_fill_fg": func(hex string) { barFillFGHex = hex; colBarFill = buildBarFill() },

	"accent_amber": func(hex string) { accentAmberHex = hex; colAmber = "[#" + hex + "]" },
	"accent_teal":  func(hex string) { accentTealHex = hex; colTeal = "[#" + hex + "]" },

	"border_playlist": func(hex string) { borderColorPlaylist = mustColor(hex) },
	"border_coverart": func(hex string) { borderColorCoverArt = mustColor(hex) },
	"border_spectrum": func(hex string) { borderColorSpectrum = mustColor(hex) },

	"spectrum_low":  func(hex string) { spectrumLow = mustColor(hex) },
	"spectrum_mid":  func(hex string) { spectrumMid = mustColor(hex) },
	"spectrum_high": func(hex string) { spectrumHigh = mustColor(hex) },

	"body_bg": func(hex string) { colBodyBG = mustColor(hex) },

	"playlist_selected_fg": func(hex string) { colPlaylistSelFG = mustColor(hex) },
	"playlist_selected_bg": func(hex string) { colPlaylistSelBG = mustColor(hex) },
	"browser_selected_fg":  func(hex string) { colBrowserSelFG = mustColor(hex) },
	"browser_selected_bg":  func(hex string) { colBrowserSelBG = mustColor(hex) },
}

// applyThemeReader parses theme.conf-formatted content from r, applying
// each valid "key = #rrggbb" line to the matching package var via
// themeSetters. It returns one warning string per malformed line, unknown
// key, or invalid color value; valid lines, blank lines, and full-line
// comments (first non-whitespace char '#') produce no warning.
func applyThemeReader(r io.Reader) []string {
	var warnings []string
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			warnings = append(warnings, fmt.Sprintf("line %d: malformed line %q (want key = #rrggbb)", lineNo, line))
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		setter, ok := themeSetters[key]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("line %d: unknown key %q", lineNo, key))
			continue
		}
		if _, err := hexToColor(value); err != nil {
			warnings = append(warnings, fmt.Sprintf("line %d: %v", lineNo, err))
			continue
		}
		setter(value[1:]) // strip leading '#'
	}
	return warnings
}

// loadTheme applies $XDG_CONFIG_HOME/muzak321/theme.conf (falling back to
// ~/.config per os.UserConfigDir) over the default palette, if present.
// A missing file or directory is silent; malformed content produces
// warnings on stderr but never stops startup.
func loadTheme() {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	f, err := os.Open(filepath.Join(dir, "muzak321", "theme.conf"))
	if err != nil {
		return
	}
	defer f.Close()
	for _, w := range applyThemeReader(f) {
		fmt.Fprintln(os.Stderr, "warning: theme.conf "+w)
	}
}
