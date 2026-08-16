package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // register JPEG decoder
	_ "image/png"  // register PNG decoder
	"strings"
)

// Cover art rendering constants (half-block cells).
const (
	CoverArtWidth  = 24 // cells
	CoverArtHeight = 12 // cells (= 48x24 pixels)
)

// coverArtBlock renders image data as width×height half-block ANSI truecolor
// rows. Each output cell is one "▀" glyph: foreground = upper pixel,
// background = lower pixel, using tview dynamic-color syntax. Returns "" on
// decode failure.
func coverArtBlock(data []byte, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return ""
	}

	targetW, targetH := width*2, height*2
	pixel := func(x, y int) (uint8, uint8, uint8) {
		sx := x * srcW / targetW
		sy := y * srcH / targetH
		r, g, b, _ := img.At(bounds.Min.X+sx, bounds.Min.Y+sy).RGBA()
		return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
	}

	var sb strings.Builder
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			tr, tg, tb := pixel(x, y*2)   // upper pixel
			br, bg, bb := pixel(x, y*2+1) // lower pixel
			fmt.Fprintf(&sb, "[#%02x%02x%02x,#%02x%02x%02x]▀[-]",
				tr, tg, tb, br, bg, bb)
		}
		if y < height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
