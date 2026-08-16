package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeToneWAV writes a short valid 16-bit PCM WAV so shuffle tests are
// self-contained (no external fixtures).
func writeToneWAV(t *testing.T, path string) {
	t.Helper()
	const sr = 8000
	const samples = 1600 // 0.2 s
	data := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		v := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/sr))
		binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
	}
	var buf []byte
	buf = append(buf, []byte("RIFF")...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(36+len(data)))
	buf = append(buf, []byte("WAVEfmt ")...)
	buf = binary.LittleEndian.AppendUint32(buf, 16) // fmt chunk size
	buf = binary.LittleEndian.AppendUint16(buf, 1)  // PCM
	buf = binary.LittleEndian.AppendUint16(buf, 1)  // mono
	buf = binary.LittleEndian.AppendUint32(buf, sr)
	buf = binary.LittleEndian.AppendUint32(buf, sr*2) // byte rate
	buf = binary.LittleEndian.AppendUint16(buf, 2)    // block align
	buf = binary.LittleEndian.AppendUint16(buf, 16)   // bits per sample
	buf = append(buf, []byte("data")...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(data)))
	buf = append(buf, data...)
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func testWAVs(t *testing.T, names ...string) []string {
	t.Helper()
	dir := t.TempDir()
	var files []string
	for _, n := range names {
		p := filepath.Join(dir, n)
		writeToneWAV(t, p)
		files = append(files, p)
	}
	return files
}

func isPermutation(order []int) bool {
	seen := make([]bool, len(order))
	for _, v := range order {
		if v < 0 || v >= len(order) || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}

// TestShuffleRandomStart — shuffle must not always start at song 0: across
// many SetFiles calls the first song varies, and it is always in range.
func TestShuffleRandomStart(t *testing.T) {
	files := testWAVs(t, "a.wav", "b.wav", "c.wav", "d.wav", "e.wav")
	starts := map[int]bool{}
	for i := 0; i < 60; i++ {
		p := NewPlayer()
		p.SetFiles(files, true)
		starts[p.CurrentIndex()] = true
	}
	if len(starts) < 2 {
		t.Fatalf("shuffle always starts at the same song: %v", starts)
	}
	for s := range starts {
		if s < 0 || s >= len(files) {
			t.Fatalf("start index %d out of range", s)
		}
	}
}

// TestShuffleOrderIsPermutation — the shuffle playback order is always a
// full permutation of the playlist (every song exactly once).
func TestShuffleOrderIsPermutation(t *testing.T) {
	files := testWAVs(t, "a.wav", "b.wav", "c.wav", "d.wav", "e.wav")
	for i := 0; i < 40; i++ {
		p := NewPlayer()
		p.SetFiles(files, true)
		if !isPermutation(p.order) {
			t.Fatalf("order %v is not a permutation of 0..%d", p.order, len(files)-1)
		}
	}
}

// TestNonShuffleOrderIdentity — normal mode keeps the natural order.
func TestNonShuffleOrderIdentity(t *testing.T) {
	files := testWAVs(t, "a.wav", "b.wav", "c.wav")
	p := NewPlayer()
	p.SetFiles(files, false)
	for i, want := range []int{0, 1, 2} {
		if p.order[i] != want {
			t.Fatalf("order[%d]=%d, want %d", i, p.order[i], want)
		}
	}
}

// TestShuffleNextPreviousWalkOrder — Next walks the shuffle permutation and
// Previous backtracks through it (no re-randomization, no repeats).
func TestShuffleNextPreviousWalkOrder(t *testing.T) {
	files := testWAVs(t, "a.wav", "b.wav", "c.wav", "d.wav")
	p := NewPlayer()
	p.SetFiles(files, true)
	order := append([]int(nil), p.order...)

	// Walk all the way to the end with Next.
	for i := 1; i < len(order); i++ {
		p.Next()
		if p.CurrentIndex() != order[i] {
			t.Fatalf("after Next #%d: current file %d, want %d (order %v)", i, p.CurrentIndex(), order[i], order)
		}
	}
	// Next at the end is a no-op.
	p.Next()
	if p.CurrentIndex() != order[len(order)-1] {
		t.Fatalf("Next past the end moved to %d", p.CurrentIndex())
	}
	// Previous backtracks through the permutation.
	for i := len(order) - 2; i >= 0; i-- {
		p.Previous()
		if p.CurrentIndex() != order[i] {
			t.Fatalf("after Previous: current file %d, want %d (order %v)", p.CurrentIndex(), order[i], order)
		}
	}
	// Previous at the start is a no-op.
	p.Previous()
	if p.CurrentIndex() != order[0] {
		t.Fatalf("Previous before the start moved to %d", p.CurrentIndex())
	}
}

// TestShufflePlaybackPlaysAllOnce — a full shuffle playback starts at a
// random song, plays every song exactly once, and stops at the end.
func TestShufflePlaybackPlaysAllOnce(t *testing.T) {
	files := testWAVs(t, "a.wav", "b.wav", "c.wav")
	p := NewPlayer()
	p.initSpeak = fakeInit
	p.playSpeak = fakePlay
	p.SetFiles(files, true)
	order := append([]int(nil), p.order...)

	p.Start()
	p.PlayCurrent()

	var seen []int
	last := -1
	deadline := time.Now().Add(8 * time.Second)
	var stable time.Duration
	for time.Now().Before(deadline) {
		cur := p.CurrentIndex()
		if cur != last {
			seen = append(seen, cur)
			last = cur
		}
		if p.State() == StateStopped {
			stable += 30 * time.Millisecond
			time.Sleep(30 * time.Millisecond)
			if stable >= 300*time.Millisecond && len(seen) == len(files) {
				break
			}
		} else {
			stable = 0
			time.Sleep(10 * time.Millisecond)
		}
	}
	if len(seen) != len(files) {
		t.Fatalf("played %v, want all %d songs in order %v", seen, len(files), order)
	}
	for i, want := range order {
		if seen[i] != want {
			t.Fatalf("playback order %v does not match shuffle order %v", seen, order)
		}
	}
}
