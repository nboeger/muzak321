package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
)

func samplesToDuration(samples int, sr beep.SampleRate) time.Duration {
	if sr == 0 {
		return 0
	}
	return time.Second * time.Duration(samples) / time.Duration(sr)
}

type PlayerState int

const (
	StateStopped PlayerState = iota
	StatePlaying
	StatePaused
	StateMuted
)

type volStreamer struct {
	mu     sync.Mutex
	inner  beep.Streamer
	volume float64
}

func (v *volStreamer) Stream(samples [][2]float64) (int, bool) {
	v.mu.Lock()
	vol := v.volume
	v.mu.Unlock()

	n, ok := v.inner.Stream(samples)
	if ok && vol != 1.0 {
		for i := range samples[:n] {
			samples[i][0] *= float64(vol)
			samples[i][1] *= float64(vol)
		}
	}
	return n, ok
}

func (v *volStreamer) Err() error {
	return v.inner.Err()
}

type Player struct {
	mu         sync.RWMutex
	state      PlayerState
	files      []string
	currentIdx int
	shuffle    bool
	done       []int

	nextRequested bool
	errorMsg      string
	volume        float64
	muted         bool
	lastVolume    float64

	ctrl        *beep.Ctrl
	volStr      *volStreamer
	abort       chan struct{}
	speakerInit bool
	streamer    beep.StreamSeekCloser
	sampleRate  beep.SampleRate

	stopCh      chan struct{}
	fileChanged chan string
	stateChan   chan PlayerState
	playNext    chan struct{}

	rng seededRand
}

func NewPlayer() *Player {
	return &Player{
		state:       StateStopped,
		volume:      0.8,
		stopCh:      make(chan struct{}),
		fileChanged: make(chan string, 4),
		stateChan:   make(chan PlayerState, 4),
		playNext:    make(chan struct{}, 1),
	}
}

func (p *Player) Play() {
	if p.muted {
		p.mu.Lock()
		p.muted = false
		p.mu.Unlock()
	}
	if p.ctrl != nil {
		p.ctrl.Paused = false
	}
	p.setState(StatePlaying)
}

func (p *Player) Pause() {
	if p.ctrl != nil {
		p.ctrl.Paused = true
	}
	p.setState(StatePaused)
}

func (p *Player) Mute() {
	p.mu.Lock()
	p.muted = true
	p.lastVolume = p.volume
	if p.volStr != nil {
		p.volStr.mu.Lock()
		p.volStr.volume = 0
		p.volStr.mu.Unlock()
	}
	p.mu.Unlock()
	p.setState(StateMuted)
}

func (p *Player) Unmute() {
	p.mu.Lock()
	p.muted = false
	if p.volStr != nil {
		p.volStr.mu.Lock()
		p.volStr.volume = p.volume
		p.volStr.mu.Unlock()
	}
	p.mu.Unlock()
	p.setState(StatePlaying)
}

func (p *Player) State() PlayerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

func (p *Player) Error() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.errorMsg
}

func (p *Player) Volume() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.volume
}

func (p *Player) SetVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 2.0 {
		v = 2.0
	}
	p.mu.Lock()
	p.volume = v
	if !p.muted && p.volStr != nil {
		p.volStr.mu.Lock()
		p.volStr.volume = v
		p.volStr.mu.Unlock()
	}
	p.mu.Unlock()
}

func (p *Player) DeviceName() string  { return "beep" }
func (p *Player) DeviceCount() int    { return 0 }
func (p *Player) DeviceIndex() int    { return 0 }
func (p *Player) SetDevice(int)       {}

func (p *Player) Progress() (pos, dur time.Duration) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.streamer != nil {
		sr := p.sampleRate
		pos = samplesToDuration(p.streamer.Position(), sr)
		dur = samplesToDuration(p.streamer.Len(), sr)
	}
	return
}

func (p *Player) CurrentFile() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.currentIdx < 0 || p.currentIdx >= len(p.files) {
		return ""
	}
	return p.files[p.currentIdx]
}

func (p *Player) Files() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.files
}

func (p *Player) CurrentIndex() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.currentIdx
}

func (p *Player) SetFiles(files []string, shuffle bool) {
	p.mu.Lock()
	p.files = files
	p.shuffle = shuffle
	p.currentIdx = 0
	if shuffle {
		p.done = make([]int, 0, len(files))
	} else {
		p.done = nil
	}
	p.mu.Unlock()
}

func (p *Player) AppendFiles(files []string) {
	p.mu.Lock()
	wasStopped := p.state == StateStopped
	p.files = append(p.files, files...)
	if wasStopped && len(p.files) > 0 {
		p.currentIdx = len(p.files) - len(files)
	}
	p.mu.Unlock()
	if wasStopped {
		select {
		case p.playNext <- struct{}{}:
		default:
		}
	}
}

func (p *Player) Start() {
	p.mu.Lock()
	p.stopCh = make(chan struct{})
	p.mu.Unlock()
	go p.playbackLoop()
}

func (p *Player) Stop() {
	if p.ctrl != nil {
		p.ctrl.Paused = false
	}
	p.closeDecoderLocked()

	select {
	case <-p.stopCh:
	default:
		close(p.stopCh)
	}
}

func (p *Player) setState(s PlayerState) {
	p.mu.Lock()
	p.state = s
	p.mu.Unlock()
	p.sendState(s)
}

func (p *Player) sendState(s PlayerState) {
	select {
	case p.stateChan <- s:
	default:
	}
}

func (p *Player) playbackLoop() {
	for {
		select {
		case <-p.stopCh:
			return
		case <-p.playNext:
			p.playCurrent()
		}
	}
}

func (p *Player) closeDecoderLocked() {
	if p.abort != nil {
		select {
		case <-p.abort:
		default:
			close(p.abort)
		}
		p.abort = nil
	}
	p.ctrl = nil
	p.volStr = nil
	p.streamer = nil
	p.sampleRate = 0
	p.state = StateStopped
}

func (p *Player) playCurrent() {
	p.mu.Lock()
	if len(p.files) == 0 {
		p.state = StateStopped
		p.mu.Unlock()
		p.sendState(StateStopped)
		return
	}

	p.closeDecoderLocked()
	file := p.files[p.currentIdx]
	p.mu.Unlock()

	audioFile, err := os.Open(file)
	if err != nil {
		p.mu.Lock()
		p.errorMsg = err.Error()
		p.mu.Unlock()
		p.sendState(StateStopped)
		return
	}

	streamer, format, err := mp3.Decode(audioFile)
	if err != nil {
		audioFile.Close()
		p.mu.Lock()
		p.errorMsg = fmt.Sprintf("invalid MP3 file: %v", err)
		p.mu.Unlock()
		p.sendState(StateStopped)
		return
	}

	if !p.speakerInit {
		if err := speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10)); err != nil {
			streamer.Close()
			p.mu.Lock()
			p.errorMsg = fmt.Sprintf("speaker init: %v", err)
			p.mu.Unlock()
			p.sendState(StateStopped)
			return
		}
		p.speakerInit = true
	}

	p.mu.Lock()
	p.streamer = streamer
	p.sampleRate = format.SampleRate
	p.mu.Unlock()

	volStr := &volStreamer{inner: streamer, volume: p.volume}
	ctrl := &beep.Ctrl{Streamer: volStr}
	trackDone := make(chan struct{})
	abort := make(chan struct{})

	speaker.Play(beep.Seq(ctrl, beep.Callback(func() {
		close(trackDone)
	})))

	p.mu.Lock()
	p.ctrl = ctrl
	p.volStr = volStr
	p.abort = abort
	if p.muted {
		p.volStr.mu.Lock()
		p.volStr.volume = 0
		p.volStr.mu.Unlock()
	}
	p.state = StatePlaying
	p.errorMsg = ""
	p.mu.Unlock()

	select {
	case p.fileChanged <- file:
	default:
	}

	select {
	case <-trackDone:
	case <-abort:
	case <-p.stopCh:
	}

	streamer.Close()
	audioFile.Close()

	// Wait for speaker to flush the streamer before starting the next track
	time.Sleep(100 * time.Millisecond)

	p.mu.Lock()
	p.closeDecoderLocked()
	advance := p.nextRequested
	p.nextRequested = false
	isDone := false
	if !advance {
		if (!p.shuffle && p.currentIdx+1 >= len(p.files)) ||
			(p.shuffle && len(p.done) >= len(p.files)) {
			isDone = true
		}
	}
	p.mu.Unlock()

	if advance || isDone {
		if isDone {
			p.mu.Lock()
			p.state = StateStopped
			p.mu.Unlock()
			p.sendState(StateStopped)
		}
		return
	}

	p.Next()
}

func (p *Player) startNext() {
	p.mu.Lock()
	p.nextRequested = true
	p.closeDecoderLocked()
	p.mu.Unlock()

	select {
	case p.playNext <- struct{}{}:
	default:
	}
}

func (p *Player) Previous() {
	p.mu.Lock()
	if p.shuffle {
		if len(p.done) == 0 {
			p.mu.Unlock()
			return
		}
		idx := p.rng.Intn(len(p.files))
		for {
			used := false
			for _, d := range p.done {
				if d == idx {
					used = true
					break
				}
			}
			if !used {
				p.currentIdx = idx
				p.done = append(p.done, idx)
				break
			}
			idx = p.rng.Intn(len(p.files))
		}
	} else {
		if p.currentIdx-1 < 0 {
			p.mu.Unlock()
			return
		}
		p.currentIdx--
	}
	p.mu.Unlock()
	p.startNext()
}

func (p *Player) Next() {
	p.mu.Lock()
	if p.shuffle {
		if len(p.done) >= len(p.files) {
			p.mu.Unlock()
			return
		}
		for {
			idx := p.rng.Intn(len(p.files))
			used := false
			for _, d := range p.done {
				if d == idx {
					used = true
					break
				}
			}
			if !used {
				p.currentIdx = idx
				p.done = append(p.done, idx)
				break
			}
		}
	} else {
		if p.currentIdx+1 >= len(p.files) {
			p.mu.Unlock()
			return
		}
		p.currentIdx++
	}
	p.mu.Unlock()
	p.startNext()
}

func (p *Player) PlayCurrent() {
	p.setState(StatePlaying)
	select {
	case p.playNext <- struct{}{}:
	default:
	}
}

type seededRand struct {
	state uint64
}

func (r *seededRand) Intn(n int) int {
	if r.state == 0 {
		r.state = uint64(time.Now().UnixNano())
	}
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return int(r.state % uint64(n))
}
