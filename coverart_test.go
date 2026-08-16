package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// testPNG builds a 48x24 PNG with a distinct top-left and bottom-left pixel.
func testPNG(t *testing.T) []byte {
	t.Helper()
	const w, h = 48, 24
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{10, 20, 30, 255})
		}
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})     // upper-left: red
	img.Set(0, 1, color.RGBA{0, 0, 255, 255})     // lower-left: blue
	img.Set(w-1, h-1, color.RGBA{0, 255, 0, 255}) // bottom-right: green
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestCoverArtBlockRender — a generated 48x24 PNG renders to a 24x12 string of
// ▀ cells with truecolor codes; each cell's fg/bg match the sampled pixels.
func TestCoverArtBlockRender(t *testing.T) {
	s := coverArtBlock(testPNG(t), CoverArtWidth, CoverArtHeight)
	if s == "" {
		t.Fatal("coverArtBlock returned empty for valid PNG")
	}
	if got := strings.Count(s, "\n") + 1; got != CoverArtHeight {
		t.Errorf("rows = %d, want %d", got, CoverArtHeight)
	}
	if got := strings.Count(s, "▀"); got != CoverArtWidth*CoverArtHeight {
		t.Errorf("glyph cells = %d, want %d", got, CoverArtWidth*CoverArtHeight)
	}
	// Cell (0,0): upper pixel = red (255,0,0), lower = blue (0,0,255).
	if !strings.HasPrefix(s, "[#ff0000,#0000ff]▀[-]") {
		t.Errorf("cell (0,0) = %q, want [#ff0000,#0000ff]▀[-]", s[:len("[#ff0000,#0000ff]▀[-]")])
	}
}

// TestCoverArtBlockGarbage — garbage bytes → empty string, no panic.
func TestCoverArtBlockGarbage(t *testing.T) {
	if s := coverArtBlock([]byte("not an image at all"), 24, 12); s != "" {
		t.Errorf("garbage bytes rendered %q, want empty", s)
	}
}

// TestCoverArtBlockOddSizes — odd dimensions and tiny images do not panic.
func TestCoverArtBlockOddSizes(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{1, 2, 3, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	for _, tc := range []struct{ w, h int }{
		{1, 1}, {7, 5}, {24, 13}, {0, 0},
	} {
		if s := coverArtBlock(data, tc.w, tc.h); tc.w > 0 && tc.h > 0 && s == "" {
			t.Errorf("coverArtBlock(1x1 png, %dx%d) rendered empty", tc.w, tc.h)
		}
	}
}

// TestSetCoverArt — empty input clears the pane; identical data is cached.
func TestSetCoverArt(t *testing.T) {
	u := NewUI()
	data := testPNG(t)
	u.SetCoverArt(data, "image/png")
	if u.coverArt.GetText(true) == "" {
		t.Error("SetCoverArt with data left the pane empty")
	}
	u.SetCoverArt(nil, "")
	if u.coverArt.GetText(true) != "" {
		t.Error("SetCoverArt(nil) should clear the pane")
	}
	u.SetCoverArt(data, "image/png")
	first := u.coverArt.GetText(false)
	u.SetCoverArt(data, "image/png") // same data: cached, no re-render
	if got := u.coverArt.GetText(false); got != first {
		t.Error("SetCoverArt re-rendered identical data")
	}
}
