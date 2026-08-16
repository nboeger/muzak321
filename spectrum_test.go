package main

import (
	"math"
	"strings"
	"testing"
)

// sineSamples generates n samples of a sine tone at freq Hz.
func sineSamples(sr int, freq float64, n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = math.Sin(2 * math.Pi * freq * float64(i) / float64(sr))
	}
	return s
}

func peakBand(bands []float64) int {
	best, idx := -1.0, -1
	for i, v := range bands {
		if v > best {
			best, idx = v, i
		}
	}
	return idx
}

// TestSpectrumBandsTonePosition — a 440 Hz tone lights bands around the 440 Hz
// region; a 1 kHz tone lights a different, higher region (±1 band tolerance).
func TestSpectrumBandsTonePosition(t *testing.T) {
	const sr = 44100
	edges := spectrumBandEdges()
	tests := []struct {
		freq float64
		want int
	}{
		{440, 5},
		{1000, 7},
	}
	for _, tt := range tests {
		bands := spectrumBands(sineSamples(sr, tt.freq, SpectrumWindow), edges, sr)
		got := peakBand(bands)
		if got < tt.want-1 || got > tt.want+1 {
			t.Errorf("freq %.0f Hz: peak band = %d, want %d (±1)", tt.freq, got, tt.want)
		}
		if bands[got] < 0.9 {
			t.Errorf("freq %.0f Hz: peak value = %.3f, want >= 0.9", tt.freq, bands[got])
		}
	}
}

// TestSpectrumBandsSilence — silence produces all-zero bands.
func TestSpectrumBandsSilence(t *testing.T) {
	bands := spectrumBands(make([]float64, SpectrumWindow), spectrumBandEdges(), 44100)
	for i, v := range bands {
		if v != 0 {
			t.Fatalf("silence band %d = %v, want 0", i, v)
		}
	}
}

// TestSpectrumBandsDC — DC produces a band-0-heavy frame.
func TestSpectrumBandsDC(t *testing.T) {
	dc := make([]float64, SpectrumWindow)
	for i := range dc {
		dc[i] = 1.0
	}
	bands := spectrumBands(dc, spectrumBandEdges(), 44100)
	if peakBand(bands) != 0 {
		t.Fatalf("DC: peak band = %d, want 0", peakBand(bands))
	}
	if bands[0] < 0.5 {
		t.Fatalf("DC: band 0 = %v, want >= 0.5 (band-0-heavy)", bands[0])
	}
}

// TestSampleTapWindow — newest-N in chronological order, wrapping past capacity.
func TestSampleTapWindow(t *testing.T) {
	tap := newSampleTap(8)
	tap.write([]float64{1, 2, 3, 4})
	w := tap.window(8)
	if len(w) != 4 || w[0] != 1 || w[3] != 4 {
		t.Fatalf("partial window = %v, want [1 2 3 4]", w)
	}
	tap.write([]float64{5, 6, 7, 8, 9}) // 9 total > 8 capacity
	w = tap.window(8)
	want := []float64{2, 3, 4, 5, 6, 7, 8, 9}
	for i := range want {
		if w[i] != want[i] {
			t.Fatalf("wrapped window = %v, want %v", w, want)
		}
	}
	w2 := tap.window(3)
	if len(w2) != 3 || w2[0] != 7 || w2[2] != 9 {
		t.Fatalf("newest-3 = %v, want [7 8 9]", w2)
	}
}

// TestSpectrumSmoothing — a step input converges within ~10 frames; a dropped
// signal decays to ~0 within ~30 frames.
func TestSpectrumSmoothing(t *testing.T) {
	p := &Player{
		tap:        newSampleTap(SpectrumWindow),
		sampleRate: 44100,
		bandEdges:  spectrumBandEdges(),
		state:      StatePlaying,
	}
	// Step: silence, then a 440 Hz tone.
	p.tap.write(make([]float64, SpectrumWindow))
	_ = p.Spectrum() // frame 0: all zeros
	tone := sineSamples(44100, 440, SpectrumWindow)
	var peak float64
	for i := 0; i < 10; i++ {
		p.tap.write(tone)
		bands := p.Spectrum()
		peak = bands[peakBand(bands)]
	}
	if peak < 0.9 {
		t.Fatalf("step input: peak after 10 frames = %.3f, want >= 0.9", peak)
	}

	// Dropped signal decays toward ~0 (check the final frame, not the max
	// across frames — the first silence frame still shows the 0.15 blend).
	silence := make([]float64, SpectrumWindow)
	var last []float64
	for i := 0; i < 30; i++ {
		p.tap.write(silence)
		last = p.Spectrum()
	}
	for i, v := range last {
		if v >= 0.01 {
			t.Fatalf("decay: band %d after 30 silence frames = %.4f, want < 0.01", i, v)
		}
	}
}

// TestSetSpectrumRender — 14 columns × 3 rows of block glyphs with truecolor
// codes when active; dimmed baseline when paused; clears when stopped.
func TestSetSpectrumRender(t *testing.T) {
	u := NewUI()
	vals := make([]float64, 14)
	for i := range vals {
		vals[i] = 1.0
	}
	u.SetSpectrum(vals, true)
	text := u.spectrum.GetText(false)
	if !strings.Contains(text, "█") {
		t.Errorf("active render missing block glyphs: %q", text)
	}
	if !strings.Contains(text, "#") {
		t.Errorf("active render missing truecolor codes: %q", text)
	}
	if strings.Count(text, "\n") != 2 {
		t.Errorf("want 3 rows, got %q", text)
	}

	// Mid-level value: bottom row full, middle row partial (▄).
	half := make([]float64, 14)
	for i := range half {
		half[i] = 0.5
	}
	u.SetSpectrum(half, true)
	if !strings.Contains(u.spectrum.GetText(false), "▄") {
		t.Errorf("0.5 value should render a partial block (▄)")
	}

	u.SetSpectrum(vals, false)
	text = u.spectrum.GetText(false)
	if strings.Contains(text, "█") {
		t.Errorf("inactive render should be dimmed, got %q", text)
	}

	u.SetSpectrum(nil, false)
	if u.spectrum.GetText(true) != "" {
		t.Errorf("stopped render should clear, got %q", u.spectrum.GetText(true))
	}
}
