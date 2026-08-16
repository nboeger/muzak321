package main

import (
	"testing"
	"time"

	"github.com/faiface/beep"
)

// fakeSeekCloser is a seekable in-memory streamer for seek tests.
type fakeSeekCloser struct {
	pos int
	len int
}

func (f *fakeSeekCloser) Stream(samples [][2]float64) (int, bool) {
	return 0, false
}
func (f *fakeSeekCloser) Err() error       { return nil }
func (f *fakeSeekCloser) Len() int         { return f.len }
func (f *fakeSeekCloser) Position() int    { return f.pos }
func (f *fakeSeekCloser) Seek(n int) error { f.pos = n; return nil }
func (f *fakeSeekCloser) Close() error     { return nil }

var _ beep.StreamSeekCloser = (*fakeSeekCloser)(nil)

// TestSeekClamps — negative seeks clamp to 0, beyond-end seeks clamp to the
// duration, and Progress reports the seeked position.
func TestSeekClamps(t *testing.T) {
	sr := beep.SampleRate(44100)
	dur := 30 * time.Second
	sc := &fakeSeekCloser{len: sr.N(dur)}
	p := &Player{sampleRate: sr, streamer: sc, state: StatePlaying}

	if err := p.SeekTo(-5 * time.Second); err != nil {
		t.Fatalf("SeekTo(-5s): %v", err)
	}
	if pos, _ := p.Progress(); pos != 0 {
		t.Errorf("negative seek: pos = %v, want 0", pos)
	}

	if err := p.SeekTo(time.Hour); err != nil {
		t.Fatalf("SeekTo(1h): %v", err)
	}
	if pos, d := p.Progress(); pos != dur || d != dur {
		t.Errorf("beyond-end seek: pos/dur = %v/%v, want %v/%v", pos, d, dur, dur)
	}

	if err := p.SeekTo(10 * time.Second); err != nil {
		t.Fatalf("SeekTo(10s): %v", err)
	}
	if pos, _ := p.Progress(); pos != 10*time.Second {
		t.Errorf("seek: pos = %v, want 10s", pos)
	}
}

// TestSeekRelative — ±delta moves from the current position, clamped.
func TestSeekRelative(t *testing.T) {
	sr := beep.SampleRate(44100)
	sc := &fakeSeekCloser{len: sr.N(60 * time.Second)}
	p := &Player{sampleRate: sr, streamer: sc, state: StatePlaying}

	if err := p.SeekTo(50 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := p.SeekRelative(20 * time.Second); err != nil {
		t.Fatal(err)
	}
	if pos, _ := p.Progress(); pos != 60*time.Second {
		t.Errorf("SeekRelative(+20s from 50s) = %v, want 60s (clamped)", pos)
	}
	if err := p.SeekRelative(-SeekStep); err != nil {
		t.Fatal(err)
	}
	if pos, _ := p.Progress(); pos != 55*time.Second {
		t.Errorf("SeekRelative(-5s from 60s) = %v, want 55s", pos)
	}
}

// TestSeekLiveStream — seeking a live stream returns a clear error and does
// not disturb the stream (position untouched, decoder left in place).
func TestSeekLiveStream(t *testing.T) {
	ms := &mp3Stream{sampleRate: 44100}
	sc := &fakeSeekCloser{len: 44100, pos: 100}
	p := &Player{sampleRate: 44100, streamer: sc, curMP3Stream: ms, state: StatePlaying}

	err := p.SeekTo(5 * time.Second)
	if err == nil {
		t.Fatal("SeekTo on live stream: want error, got nil")
	}
	if sc.pos != 100 {
		t.Errorf("live stream seek disturbed the decoder: pos = %d, want 100", sc.pos)
	}
	if p.curMP3Stream != ms {
		t.Errorf("live stream seek disturbed the stream connection")
	}
}

// TestSeekNoDecoder — no decoder open returns a clear error.
func TestSeekNoDecoder(t *testing.T) {
	p := &Player{state: StateStopped}
	if err := p.SeekTo(time.Second); err == nil {
		t.Fatal("SeekTo with no decoder: want error, got nil")
	}
}

// TestSeekWhilePaused — seeking works while paused (the decoder is still open).
func TestSeekWhilePaused(t *testing.T) {
	sr := beep.SampleRate(44100)
	sc := &fakeSeekCloser{len: sr.N(60 * time.Second)}
	p := &Player{sampleRate: sr, streamer: sc, state: StatePaused}
	if err := p.SeekTo(7 * time.Second); err != nil {
		t.Fatalf("seek while paused: %v", err)
	}
	if pos, _ := p.Progress(); pos != 7*time.Second {
		t.Errorf("seek while paused: pos = %v, want 7s", pos)
	}
}
