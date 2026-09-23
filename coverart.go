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
	"sync"

	"github.com/BourgeoisBear/rasterm"
	"github.com/gdamore/tcell/v2"
)

// graphicsProtocol identifies which inline-image protocol the terminal
// supports, detected once and cached (Sixel detection queries the terminal
// and must not run on every draw tick).
var (
	gfxProtoOnce sync.Once
	gfxProto     string // "kitty", "iterm", "sixel", or "" (none)
)

func detectGraphicsProtocol() string {
	gfxProtoOnce.Do(func() {
		switch {
		case rasterm.IsKittyCapable():
			gfxProto = "kitty"
		case rasterm.IsItermCapable():
			gfxProto = "iterm"
		default:
			if ok, _ := rasterm.IsSixelCapable(); ok {
				gfxProto = "sixel"
			}
		}
	})
	return gfxProto
}

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

// coverArtPayload renders data using the detected inline-graphics protocol,
// sized to width x height terminal cells. ok is false if the terminal
// supports no graphics protocol, or the image cannot be decoded/encoded.
func coverArtPayload(data []byte, width, height int) (payload string, ok bool) {
	proto := detectGraphicsProtocol()
	if proto == "" {
		return "", false
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", false
	}

	var out bytes.Buffer
	switch proto {
	case "kitty":
		opts := rasterm.KittyImgOpts{DstCols: uint32(width), DstRows: uint32(height)}
		if err := rasterm.KittyWriteImage(&out, img, opts); err != nil {
			return "", false
		}
	case "iterm":
		opts := rasterm.ItermImgOpts{
			Width:         fmt.Sprintf("%d", width),
			Height:        fmt.Sprintf("%d", height),
			DisplayInline: true,
		}
		if err := rasterm.ItermWriteImageWithOptions(&out, img, opts); err != nil {
			return "", false
		}
	case "sixel":
		if err := coverArtSixel(data, &out); err != nil {
			return "", false
		}
	}
	return out.String(), true
}

// coverArtDrawFunc returns a tview SetDrawFunc that emits the cover image
// via the terminal's native inline-graphics protocol (Kitty / iTerm2 /
// Sixel), positioned at the box's inner (post-border) cell. Returns nil if
// the terminal supports no graphics protocol or the image can't be encoded,
// so the caller can fall back to coverArtBlock ASCII rendering.
func coverArtDrawFunc(data []byte, width, height int) func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
	payload, ok := coverArtPayload(data, width, height)
	if !ok {
		return nil
	}
	return func(screen tcell.Screen, x, y, w, h int) (int, int, int, int) {
		innerX, innerY := x+1, y+1
		innerW, innerH := w-2, h-2
		// Position the cursor at the box's inner top-left (1-indexed ANSI
		// CUP) before emitting the graphics escape sequence, since these
		// protocols place the image relative to the current cursor cell.
		fmt.Fprintf(os.Stdout, "\x1b[%d;%dH", innerY+1, innerX+1)
		os.Stdout.WriteString(payload)
		return innerX, innerY, innerW, innerH
	}
}
