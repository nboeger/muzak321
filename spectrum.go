package main

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

// Spectrum constants.
const (
	SpectrumWindow    = 4096    // FFT window: ~93 ms at 44.1 kHz
	SpectrumBands     = 14      // log-spaced output bands
	SpectrumBandMinHz = 40.0    // lowest band edge
	SpectrumBandMaxHz = 16000.0 // highest band edge
	SpectrumAttack    = 0.6     // blend toward rising values
	SpectrumDecay     = 0.85    // blend toward falling values
	SpectrumDbFloor   = 60.0    // -60 dB maps to 0 (silence ~= 0)
)

// sampleTap keeps the most recent window of mono samples as a ring buffer.
// The speaker goroutine writes; the UI goroutine reads.
type sampleTap struct {
	mu   sync.Mutex
	buf  []float64
	pos  int // next write index (ring)
	full bool
}

func newSampleTap(size int) *sampleTap {
	return &sampleTap{buf: make([]float64, size)}
}

// write appends samples, keeping only the most recent len(buf).
func (t *sampleTap) write(samples []float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, s := range samples {
		t.buf[t.pos] = s
		t.pos++
		if t.pos == len(t.buf) {
			t.pos = 0
			t.full = true
		}
	}
}

// window returns the newest n samples, oldest first, as a copy. It returns
// fewer than n samples until the buffer has been written n times.
func (t *sampleTap) window(n int) []float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) == 0 || n <= 0 {
		return nil
	}
	if n > len(t.buf) {
		n = len(t.buf)
	}
	avail := len(t.buf)
	if !t.full {
		avail = t.pos
	}
	if n > avail {
		n = avail
	}
	if n == 0 {
		return nil
	}
	out := make([]float64, n)
	start := t.pos - n
	if start < 0 {
		start += len(t.buf)
	}
	for i := 0; i < n; i++ {
		out[i] = t.buf[(start+i)%len(t.buf)]
	}
	return out
}

func (t *sampleTap) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pos = 0
	t.full = false
	clear(t.buf)
}

// spectrumBandEdges returns the SpectrumBands+1 log-spaced band edges from
// SpectrumBandMinHz to SpectrumBandMaxHz.
func spectrumBandEdges() []float64 {
	edges := make([]float64, SpectrumBands+1)
	ratio := SpectrumBandMaxHz / SpectrumBandMinHz
	for i := 0; i <= SpectrumBands; i++ {
		edges[i] = SpectrumBandMinHz * math.Pow(ratio, float64(i)/SpectrumBands)
	}
	return edges
}

// bandForFreq maps a frequency in Hz to a band index, clamping out-of-range
// values (DC and everything above the top edge land in the end bands).
func bandForFreq(hz float64, edges []float64) int {
	b := sort.SearchFloat64s(edges, hz) - 1
	if b < 0 {
		b = 0
	}
	if b >= len(edges)-1 {
		b = len(edges) - 2
	}
	return b
}

// fft computes the in-place iterative radix-2 Cooley-Tukey FFT of re/im
// (length must be a power of two).
func fft(re, im []float64) {
	n := len(re)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wr, wi := math.Cos(ang), math.Sin(ang)
		for i := 0; i < n; i += length {
			cr, ci := 1.0, 0.0
			for k := i; k < i+length/2; k++ {
				ur, ui := re[k], im[k]
				vr := cr*re[k+length/2] - ci*im[k+length/2]
				vi := cr*im[k+length/2] + ci*re[k+length/2]
				re[k] = ur + vr
				im[k] = ui + vi
				re[k+length/2] = ur - vr
				im[k+length/2] = ui - vi
				cr, ci = cr*wr-ci*wi, cr*wi+ci*wr
			}
		}
	}
}

// spectrumBands returns len(bandEdges)-1 normalized magnitudes in [0,1].
// The samples are Hann-windowed and FFT'd; bin magnitudes are summed per
// log-spaced band, normalized by the window total, scaled by 20*log10 with
// a -60 dB floor, and clamped to [0,1]. Silence maps to all zeros.
func spectrumBands(samples []float64, bandEdges []float64, sampleRate int) []float64 {
	n := len(samples)
	nBands := len(bandEdges) - 1
	out := make([]float64, nBands)
	if nBands <= 0 || n == 0 || sampleRate <= 0 {
		return out
	}

	re := make([]float64, n)
	im := make([]float64, n)
	for i := 0; i < n; i++ {
		w := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
		re[i] = samples[i] * w
	}
	fft(re, im)

	bandSum := make([]float64, nBands)
	binHz := float64(sampleRate) / float64(n)
	for k := 0; k <= n/2; k++ {
		mag := math.Hypot(re[k], im[k])
		bandSum[bandForFreq(float64(k)*binHz, bandEdges)] += mag
	}
	total := 0.0
	for _, s := range bandSum {
		total += s
	}
	if total < 1e-12 {
		return out // silence
	}
	for i, s := range bandSum {
		frac := s / total
		v := (20*math.Log10(frac) + SpectrumDbFloor) / SpectrumDbFloor
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		out[i] = v
	}
	return out
}

// spectrumColor returns a green→yellow→red truecolor hex string for a value
// in [0,1].
func spectrumColor(v float64) string {
	var r, g int
	switch {
	case v < 0.5:
		r = int(255 * v * 2)
		g = 255
	default:
		r = 255
		g = int(255 * (1 - (v-0.5)*2))
	}
	return fmt.Sprintf("%02x%02x%02x", r, g, 0)
}
