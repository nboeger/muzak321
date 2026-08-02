package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/faiface/beep"
)

// fakeInit is a no-op init for tests.
func fakeInit(sr beep.SampleRate, bufferSize int) error { return nil }

// fakePlay mimics speaker.Play: asynchronous pull of the streamer to completion,
// which fires the Seq callback (closing trackDone) exactly like real hardware.
func fakePlay(s beep.Streamer) {
	go func() {
		buf := make([][2]float64, 2048)
		for {
			n, ok := s.Stream(buf)
			_ = n
			if !ok {
				return
			}
		}
	}()
}

func waitStopped(t *testing.T, p *Player, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	var stable time.Duration
	for time.Now().Before(deadline) {
		if p.State() == StateStopped {
			stable += 30 * time.Millisecond
			time.Sleep(30 * time.Millisecond)
			if stable >= 300*time.Millisecond {
				return
			}
		} else {
			stable = 0
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatalf("player did not stop; state=%v currentIdx=%d", p.State(), p.CurrentIndex())
}

func waitPlaying(t *testing.T, p *Player) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for p.State() != StatePlaying && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p.State() != StatePlaying {
		t.Fatalf("player not playing; state=%v", p.State())
	}
}

func waitCurrent(t *testing.T, p *Player, want int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for p.CurrentIndex() != want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if p.CurrentIndex() != want {
		t.Fatalf("currentIdx=%d, want %d", p.CurrentIndex(), want)
	}
}

func TestAutoAdvanceToEnd(t *testing.T) {
	dir := t.TempDir()
	src := "/tmp/opencode/muzaktest/tone.mp3"
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("no test mp3: %v", err)
	}
	var files []string
	for _, n := range []string{"a.mp3", "b.mp3", "c.mp3"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}

	p := NewPlayer()
	p.initSpeak = fakeInit
	p.playSpeak = fakePlay

	p.SetFiles(files, false)
	p.Start()
	p.PlayCurrent()

	// Give the loop time to advance through all 3 real, decoded tracks. Stop is
	// "real" when it has been stable for a while (ignoring the transient Stopped
	// that appears briefly between tracks).
	deadline := time.Now().Add(5 * time.Second)
	var stable time.Duration
	for {
		if time.Now().After(deadline) {
			t.Fatal("playback did not reach end within timeout; currentIdx=", p.CurrentIndex())
		}
		if p.State() == StateStopped {
			stable += 30 * time.Millisecond
			time.Sleep(30 * time.Millisecond)
			if stable >= 300*time.Millisecond && p.CurrentIndex() == len(files)-1 {
				break
			}
		} else {
			stable = 0
			time.Sleep(10 * time.Millisecond)
		}
	}
	if got := p.CurrentIndex(); got != len(files)-1 {
		t.Fatalf("stopped at index %d, want %d (last); error=%q", got, len(files)-1, p.Error())
	}
}

func TestManualNextAdvances(t *testing.T) {
	dir := t.TempDir()
	src := "/tmp/opencode/muzaktest/tone.mp3"
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skipf("no test mp3: %v", err)
	}
	var files []string
	for _, n := range []string{"a.mp3", "b.mp3", "c.mp3"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}

	p := NewPlayer()
	p.initSpeak = fakeInit
	p.playSpeak = fakePlay
	p.SetFiles(files, false)
	p.Next() // request a jump while idle; must advance and keep playing

	p.Start()
	p.PlayCurrent()
	waitCurrent(t, p, 1, 5*time.Second)

	// It must continue to the last track and stop there (no early stop).
	waitCurrent(t, p, 2, 5*time.Second)
	waitStopped(t, p, 5*time.Second)
}
