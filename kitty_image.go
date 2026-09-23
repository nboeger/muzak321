package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
)

// renderKittyImage encodes img as Kitty graphics protocol.
// Returns the complete escape sequence to send to terminal.
func renderKittyImage(img image.Image, width, height int) string {
	if img == nil || width <= 0 || height <= 0 {
		return ""
	}

	// Resize to target dimensions
	resized := resizeImage(img, width, height)

	// Encode as JPEG
	var jpegBuf bytes.Buffer
	jpeg.Encode(&jpegBuf, resized, &jpeg.Options{Quality: 85})

	// Base64 encode
	b64 := base64.StdEncoding.EncodeToString(jpegBuf.Bytes())

	// Kitty graphics protocol
	return fmt.Sprintf("\x1b_Gf=100,s=%d,v=%d,a=s:\n%s\x1b_G;m=0\n\x1b\\",
		width, height, b64)
}

// resizeImage scales img to width x height using nearest-neighbor.
func resizeImage(src image.Image, w, h int) image.Image {
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			srcX := x * srcW / w
			srcY := y * srcH / h
			dst.Set(x, y, src.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}
	return dst
}
