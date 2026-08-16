package main

import (
	"fmt"
	"io"
	"math/rand/v2"
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
	"github.com/dhowden/tag"
)

func samplesToDuration(samples int, sr beep.SampleRate) time.Duration {
	if sr == 0 {
		return 0
	}
	return time.Second * time.Duration(samples) / time.Duration(sr)
}

// Seek step sizes for the player view keybindings.
const (
	SeekStep      = 5 * time.Second
	SeekStepLarge = 30 * time.Second
)

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
	tap    *sampleTap // nil when disabled; set on the player's streamers
}

func (v *volStreamer) Stream(samples [][2]float64) (int, bool) {
	v.mu.Lock()
	vol := v.volume
	tap := v.tap
	v.mu.Unlock()

	n, ok := v.inner.Stream(samples)
	// Tap the RAW samples (before the volume multiply): mono-mix into the
	// spectrum ring buffer. No-op when nil; a memcpy at most on the audio path.
	if tap != nil && n > 0 {
		mono := make([]float64, n)
		for i := range samples[:n] {
			mono[i] = (samples[i][0] + samples[i][1]) / 2
		}
		tap.write(mono)
	}
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
	order      []int

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

	coverData []byte
	coverMIME string

	lyrics     []LyricLine
	lyricsPath string

	tap          *sampleTap
	bandEdges    []float64
	spectrumPrev []float64

	stopCh      chan struct{}
	started     bool
	fileChanged chan string
	stateChan   chan PlayerState
	playNext    chan struct{}
	metaChan    chan struct{}

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
		tap:         newSampleTap(SpectrumWindow),
		bandEdges:   spectrumBandEdges(),
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

// Spectrum returns a smoothed SpectrumBands-wide frame in [0,1], or nil when
// stopped / before enough audio has been tapped. Attack/decay smoothing blends
// against the previous frame: fast attack (SpectrumAttack) on the rise, slower
// decay (SpectrumDecay) on the fall.
func (p *Player) Spectrum() []float64 {
	p.mu.RLock()
	state := p.state
	tap := p.tap
	sr := p.sampleRate
	p.mu.RUnlock()
	if state == StateStopped || tap == nil || sr == 0 {
		return nil
	}
	win := tap.window(SpectrumWindow)
	if len(win) < SpectrumWindow {
		return nil
	}
	bands := spectrumBands(win, p.bandEdges, int(sr))

	p.mu.Lock()
	prev := p.spectrumPrev
	var out []float64
	if len(prev) != len(bands) {
		out = bands
	} else {
		out = make([]float64, len(bands))
		for i, v := range bands {
			if v >= prev[i] {
				out[i] = SpectrumAttack*v + (1-SpectrumAttack)*prev[i]
			} else {
				out[i] = SpectrumDecay*v + (1-SpectrumDecay)*prev[i]
			}
		}
	}
	p.spectrumPrev = out
	p.mu.Unlock()
	return out
}

// SeekTo seeks to an absolute position, clamped to [0, duration]. Returns an
// error on live streams (curMP3Stream != nil) or when no decoder is open.
func (p *Player) SeekTo(pos time.Duration) error {
	p.mu.RLock()
	streamer := p.streamer
	live := p.curMP3Stream != nil
	sr := p.sampleRate
	p.mu.RUnlock()
	if live {
		return fmt.Errorf("seeking is not supported on live streams")
	}
	if streamer == nil || sr == 0 {
		return fmt.Errorf("no track is loaded")
	}
	samples := sr.N(pos)
	if samples < 0 {
		samples = 0
	}
	if samples > streamer.Len() {
		samples = streamer.Len()
	}
	if err := streamer.Seek(samples); err != nil {
		return err
	}
	// Emit a UI refresh so the progress bar jumps immediately.
	select {
	case p.fileChanged <- p.CurrentFile():
	default:
	}
	return nil
}

// SeekRelative seeks by ±delta (negative allowed), clamped.
func (p *Player) SeekRelative(delta time.Duration) error {
	pos, _ := p.Progress()
	return p.SeekTo(pos + delta)
}

// CoverArt returns the current file's embedded art bytes + MIME, or nil.
func (p *Player) CoverArt() ([]byte, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.coverData, p.coverMIME
}

// readCoverArt extracts the embedded picture (JPEG/PNG) from a file's tags.
func readCoverArt(path string) ([]byte, string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, ""
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, ""
	}
	pic := m.Picture()
	if pic == nil {
		return nil, ""
	}
	return pic.Data, pic.MIMEType
}

// Lyrics returns the current track's synced lyrics (empty when none).
func (p *Player) Lyrics() []LyricLine {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lyrics
}

// CurrentLyricIndex returns the index of the lyric active at the current
// playback position, or -1 before the first line.
func (p *Player) CurrentLyricIndex() int {
	pos, _ := p.Progress()
	p.mu.RLock()
	lines := p.lyrics
	p.mu.RUnlock()
	return currentLyric(lines, pos)
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
	if p.currentIdx < 0 || p.currentIdx >= len(p.order) {
		return ""
	}
	return p.files[p.order[p.currentIdx]]
}

func (p *Player) Files() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.files
}

// CurrentIndex returns the position of the current song in the playlist
// (the file index, not the position in the playback order).
func (p *Player) CurrentIndex() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.currentIdx < 0 || p.currentIdx >= len(p.order) {
		return -1
	}
	return p.order[p.currentIdx]
}

// playbackOrder returns the order of file indices to play: identity for
// normal mode, a Fisher-Yates permutation for shuffle mode. The shuffle
// permutation gives a random starting song and plays every file exactly
// once, with no rejection sampling and no modulo bias (math/rand/v2).
func playbackOrder(n int, shuffle bool) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	if !shuffle {
		return order
	}
	for i := n - 1; i > 0; i-- {
		j := rand.IntN(i + 1)
		order[i], order[j] = order[j], order[i]
	}
	return order
}

func (p *Player) SetFiles(files []string, shuffle bool) {
	p.mu.Lock()
	p.files = files
	p.shuffle = shuffle
	p.order = playbackOrder(len(files), shuffle)
	p.currentIdx = 0
	p.mu.Unlock()
}

func (p *Player) AppendFiles(files []string) {
	p.mu.Lock()
	wasStopped := p.state == StateStopped
	firstNew := len(p.files)
	p.files = append(p.files, files...)
	if wasStopped && len(p.files) > 0 {
		// Fresh start: rebuild the playback order over the combined list
		// and begin on one of the just-added files.
		p.order = playbackOrder(len(p.files), p.shuffle)
		p.currentIdx = 0
		for i, idx := range p.order {
			if idx >= firstNew {
				p.currentIdx = i
				break
			}
		}
	} else if p.shuffle {
		// Mid-cycle: extend the order with the new indices, shuffled among
		// themselves, so they play after the current cycle's remaining songs.
		start := len(p.order)
		for _, idx := range playbackOrder(len(files), true) {
			p.order = append(p.order, idx+start)
		}
	} else {
		for i := firstNew; i < len(p.files); i++ {
			p.order = append(p.order, i)
		}
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
	if p.started {
		p.mu.Unlock()
		return
	}
	p.started = true
	p.stopCh = make(chan struct{})
	p.mu.Unlock()
	go p.playbackLoop()
}

func (p *Player) Stop() {
	if p.ctrl != nil {
		p.ctrl.Paused = false
	}
	p.closeDecoderLocked()

	p.mu.Lock()
	p.started = false
	p.mu.Unlock()

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
	file := p.files[p.order[p.currentIdx]]
	// A "next" request only armed this track by advancing currentIdx (in
	// Next/Previous/startNext); it carries no meaning into playback itself, so
	// consume it now to avoid leaking it into this track's own advance logic.
	p.nextRequested = false
	// Fresh track: drop the previous track's spectrum state.
	p.spectrumPrev = nil
	p.tap.reset()
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
	if liveStream == nil {
		p.coverData, p.coverMIME = readCoverArt(file)
		lines, lerr := loadLRC(file)
		if lerr == nil {
			p.lyrics = lines
			p.lyricsPath = file
		} else {
			p.lyrics = nil
			p.lyricsPath = ""
		}
		// Recently-played history: fire-and-forget, errors surface in the
		// status bar only. Stream URLs are never recorded.
		go func() {
			if err := AppendHistory(file); err != nil {
				p.mu.Lock()
				p.errorMsg = "history: " + err.Error()
				p.mu.Unlock()
				p.sendState(p.State())
			}
		}()
	} else {
		p.coverData, p.coverMIME = nil, ""
		p.lyrics = nil
		p.lyricsPath = ""
	}
	p.mu.Unlock()

	volStr := &volStreamer{inner: streamer, volume: p.volume, tap: p.tap}
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
	if !advance && p.currentIdx+1 >= len(p.order) {
		isDone = true
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
	if p.currentIdx-1 < 0 {
		p.mu.Unlock()
		return
	}
	p.currentIdx--
	p.mu.Unlock()
	p.startNext()
}

func (p *Player) Next() {
	p.mu.Lock()
	if p.currentIdx+1 >= len(p.order) {
		p.mu.Unlock()
		return
	}
	p.currentIdx++
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

// running reports whether the playback loop is active.
func (p *Player) running() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.started
}
