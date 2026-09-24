package main

import (
	"fmt"
	"strconv"

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
