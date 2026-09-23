package main

import (
	"path/filepath"

	"github.com/gdamore/tcell/v2"
)

// Cover art dimensions (btop-style proportions)
const (
	CoverArtWidth  = 32
	CoverArtHeight = 16
)

// Border accent colors (one per panel, btop-style)
const (
	borderColorPlaylist = tcell.ColorAqua
	borderColorCoverArt = tcell.ColorFuchsia
	borderColorSpectrum = tcell.ColorGreen
)

// Spectrum rendering
const (
	spectrumRows   = 12  // vertical rows of resolution per bar
	spectrumLevels = 8   // sub-row levels per row (eighth blocks)
)

// Vertical blocks for spectrum (empty to full)
var vertBlocks = [spectrumLevels + 1]rune{
	' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█',
}

// cleanFileName returns just the base filename from a path.
func cleanFileName(path string) string {
	return filepath.Base(path)
}
