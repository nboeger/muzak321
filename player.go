package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/flac"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/vorbis"
	"github.com/faiface/beep/wav"
)

func samplesToDuration(samples int, sr beep.SampleRate) time.Duration {
	if sr == 0 {
		return 0
	}
	return time.Second * time.Duration(samples) / time.Duration(sr)
}

func decodeAudio(file *os.File, path string) (beep.StreamSeekCloser, beep.Format, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return mp3.Decode(file)
	case ".flac":
		return flac.Decode(file)
	case ".ogg":
		return vorbis.Decode(file)
	case ".wav":
		return wav.Decode(file)
	default:
		return nil, beep.Format{}, fmt.Errorf("unsupported audio format: %s", filepath.Ext(path))
	}
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
	initRate    beep.SampleRate
	streamer    beep.StreamSeekCloser
	sampleRate  beep.SampleRate

	curStream    *Stream
	curMP3Stream *mp3Stream

	stopCh      chan struct{}
	fileChanged chan string
	stateChan   chan PlayerState
	playNext    chan struct{}
	metaChan    chan struct{}
	rng         seededRand

	initSpeak func(sr beep.SampleRate, bufferSize int) error
	playSpeak func(s beep.Streamer)
}

func NewPlayer() *Player {
	return &Player{
		state:       StateStopped,
		volume:      0.8,
		stopCh:      make(chan struct{}),
		fileChanged: make(chan string, 4),
		stateChan:   make(chan PlayerState, 4),
		playNext:    make(chan struct{}, 1),
		metaChan:    make(chan struct{}, 1),
		initSpeak:   speaker.Init,
		playSpeak:   func(s beep.Streamer) { speaker.Play(s) },
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

func (p *Player) DeviceName() string { return "beep" }
func (p *Player) DeviceCount() int   { return 0 }
func (p *Player) DeviceIndex() int   { return 0 }
func (p *Player) SetDevice(int)      {}

func (p *Player) Progress() (pos, dur time.Duration) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.curMP3Stream != nil {
		pos = samplesToDuration(int(p.curMP3Stream.PlayedSamples()), p.sampleRate)
		dur = 0
		return
	}
	if p.streamer != nil {
		sr := p.sampleRate
		pos = samplesToDuration(p.streamer.Position(), sr)
		dur = samplesToDuration(p.streamer.Len(), sr)
	}
	return
}

// StreamTitle returns the live ICY StreamTitle, or "" when not on a stream.
func (p *Player) StreamTitle() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.curStream == nil {
		return ""
	}
	return p.curStream.Title()
}

func (p *Player) IsStreaming() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.curStream != nil
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

func (p *Player) sendMeta() {
	select {
	case p.metaChan <- struct{}{}:
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
	if p.curStream != nil {
		p.curStream.Close()
		p.curStream = nil
	}
	if p.curMP3Stream != nil {
		p.curMP3Stream = nil
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
	// A "next" request only armed this track by advancing currentIdx (in
	// Next/Previous/startNext); it carries no meaning into playback itself, so
	// consume it now to avoid leaking it into this track's own advance logic.
	p.nextRequested = false
	p.mu.Unlock()

	var streamer beep.Streamer
	var decoderCloser io.Closer
	var connCloser io.Closer
	var sr beep.SampleRate
	var liveStream *Stream
	var curMS *mp3Stream
	fail := func(msg string, extra ...func()) {
		for _, f := range extra {
			f()
		}
		p.mu.Lock()
		p.errorMsg = msg
		p.mu.Unlock()
		p.sendState(StateStopped)
	}

	if isStreamURL(file) {
		st, err := openStream(file)
		if err != nil {
			fail(err.Error())
			return
		}
		ms, err := newMP3Stream(st)
		if err != nil {
			st.Close()
			fail(err.Error())
			return
		}
		streamer = ms
		decoderCloser = ms
		connCloser = st
		liveStream = st
		curMS = ms
		sr = ms.SampleRate()
	} else {
		audioFile, err := os.Open(file)
		if err != nil {
			fail(err.Error())
			return
		}
		sc, format, err := decodeAudio(audioFile, file)
		if err != nil {
			audioFile.Close()
			fail(err.Error())
			return
		}
		streamer = sc
		decoderCloser = sc
		sr = format.SampleRate
	}

	if !p.speakerInit || p.initRate != sr {
		if err := p.initSpeak(sr, sr.N(time.Second/10)); err != nil {
			if connCloser != nil {
				connCloser.Close()
			}
			decoderCloser.Close()
			fail(fmt.Sprintf("speaker init: %v", err))
			return
		}
		p.speakerInit = true
		p.initRate = sr
	}

	p.mu.Lock()
	if sc, ok := streamer.(beep.StreamSeekCloser); ok {
		p.streamer = sc
	} else {
		p.streamer = nil
	}
	p.curStream = liveStream
	p.curMP3Stream = curMS
	p.sampleRate = sr
	p.mu.Unlock()

	volStr := &volStreamer{inner: streamer, volume: p.volume}
	ctrl := &beep.Ctrl{Streamer: volStr}
	trackDone := make(chan struct{})
	abort := make(chan struct{})

	p.playSpeak(beep.Seq(ctrl, beep.Callback(func() {
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

	if liveStream != nil {
		go func() {
			for {
				select {
				case <-liveStream.TitleChanged():
					p.sendMeta()
				case <-abort:
					return
				case <-p.stopCh:
					return
				}
			}
		}()
	}

	select {
	case p.fileChanged <- file:
	default:
	}

	select {
	case <-trackDone:
	case <-abort:
	case <-p.stopCh:
	}

	if connCloser != nil {
		connCloser.Close()
	}
	decoderCloser.Close()

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
	if p.curStream != nil {
		p.mu.Unlock()
		return
	}
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
