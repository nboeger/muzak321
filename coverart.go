package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

	"github.com/BourgeoisBear/rasterm"
	"github.com/gdamore/tcell/v2"
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
			fmt.Fprintf(&sb, "[#%02x%02x%02x:#%02x%02x%02x]▀[-]",
				tr, tg, tb, br, bg, bb)
		}
		if y < height-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// coverArtSixel writes the image as sixel graphics (`\033Pq…\033\`) to out
// for SIXEL-capable terminals (xterm, domterm, macterm). Returns "" on
// failure.
func coverArtSixel(data []byte, out *bytes.Buffer) error {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	pal := image.NewPaletted(img.Bounds(), nil)
	draw.FloydSteinberg.Draw(pal, img.Bounds(), img, img.Bounds().Min)
	return rasterm.SixelWriteImage(out, pal)
}

// coverArtRastern auto-detects the terminal and renders the image using the
// best available inline-graphics protocol (kitty / iTerm2 / Sixel). Returns
// "" if the terminal supports none, or the image cannot be decoded.
func coverArtRastern(data []byte) string {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	var out bytes.Buffer
	if rasterm.IsKittyCapable() {
		if err := rasterm.KittyWriteImage(&out, img, rasterm.KittyImgOpts{}); err == nil {
			return out.String()
		}
	}
	if rasterm.IsItermCapable() {
		if err := rasterm.ItermWriteImage(&out, img); err == nil {
			return out.String()
		}
	}
	if ok, _ := rasterm.IsSixelCapable(); ok {
		if err := coverArtSixel(data, &out); err == nil {
			return out.String()
		}
	}
	return ""
}

// coverArtDrawFunc returns a tview SetDrawFunc that emits the cover image
// as sixel graphics directly to the terminal. Used when the terminal is
// SIXEL-capable so the image renders as real graphics (not text).
func coverArtDrawFunc(data []byte) func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	ok, _ := rasterm.IsSixelCapable()
	if !ok {
		return nil
	}
	return func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		var out bytes.Buffer
		if coverArtSixel(data, &out) != nil {
			return x, y, width, height
		}
		// Emit the sixel payload directly to the terminal. The terminal renders
		// it as a graphic overlay over the cover-art cell area.
		os.Stdout.Write(out.Bytes())
		return x, y, width, height
	}
}
