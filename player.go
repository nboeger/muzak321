package main

import (
	"io"
	"math"
	"os"
	"sync"
	"time"

	"github.com/damonqin/portaudio"
	"github.com/hajimehoshi/go-mp3"
)

const (
	fftSize         = 1024
	numEqualizerBars = 16
)

var paInitOnce sync.Once
var paInitErr error

func initPortAudio() error {
	paInitOnce.Do(func() {
		paInitErr = portaudio.Initialize()
	})
	return paInitErr
}

type PlayerState int

const (
	StateStopped PlayerState = iota
	StatePlaying
	StatePaused
	StateMuted
)

type Player struct {
	mu         sync.RWMutex
	state      PlayerState
	files      []string
	currentIdx int
	shuffle    bool
	done       []int

	nextRequested bool
	errorMsg      string

	decoder   *mp3.Decoder
	audioFile *os.File
	paStream  *portaudio.Stream
	paBuffer  []int16

	sampleRate int

	eqData      [numEqualizerBars]float64
	stopCh      chan struct{}
	eqChan      chan [numEqualizerBars]float64
	fileChanged chan string
	stateChan   chan PlayerState
	playNext    chan struct{}
	pumpDone    chan struct{}

	rng seededRand
}

func NewPlayer() *Player {
	return &Player{
		state:       StateStopped,
		stopCh:      make(chan struct{}),
		eqChan:      make(chan [numEqualizerBars]float64, 4),
		fileChanged: make(chan string, 4),
		stateChan:   make(chan PlayerState, 4),
		playNext:    make(chan struct{}, 1),
	}
}

func (p *Player) Play()   { p.setState(StatePlaying) }
func (p *Player) Pause()  { p.setState(StatePaused) }
func (p *Player) Mute()   { p.setState(StateMuted) }
func (p *Player) Unmute() { p.setState(StatePlaying) }

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

func (p *Player) EQData() [numEqualizerBars]float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.eqData
}

func (p *Player) CurrentFile() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.currentIdx < 0 || p.currentIdx >= len(p.files) {
		return ""
	}
	return p.files[p.currentIdx]
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

func (p *Player) Start() {
	go p.playbackLoop()
}

func (p *Player) Stop() {
	p.mu.Lock()
	p.closeDecoderLocked()
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

func (p *Player) playbackLoop() {
	eqTicker := time.NewTicker(50 * time.Millisecond)
	defer eqTicker.Stop()

	for {
		select {
		case <-p.stopCh:
			p.closeDecoder()
			return
		case <-p.playNext:
			p.playCurrent()
		case <-eqTicker.C:
			p.updateEQ()
		}
	}
}

func (p *Player) closeDecoder() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeDecoderLocked()
}

func (p *Player) closeDecoderLocked() {
	if p.paStream != nil {
		p.paStream.Stop()
		p.paStream.Close()
	}
	if p.audioFile != nil {
		p.audioFile.Close()
	}
	p.decoder = nil
	p.audioFile = nil
	p.paStream = nil
	p.paBuffer = nil
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

	decoder, err := mp3.NewDecoder(audioFile)
	if err != nil {
		audioFile.Close()
		p.mu.Lock()
		p.errorMsg = "invalid MP3 file"
		p.mu.Unlock()
		p.sendState(StateStopped)
		return
	}

	sampleRate := decoder.SampleRate()

	if err := initPortAudio(); err != nil {
		audioFile.Close()
		p.mu.Lock()
		p.errorMsg = err.Error()
		p.mu.Unlock()
		p.sendState(StateStopped)
		return
	}

	const paFrames = 2048
	paBuf := make([]int16, paFrames*2)
	stream, err := portaudio.OpenDefaultStream(0, 2, float64(sampleRate), paFrames, &paBuf)
	if err != nil {
		p.mu.Lock()
		p.errorMsg = "portaudio: " + err.Error()
		p.mu.Unlock()
		audioFile.Close()
		p.sendState(StateStopped)
		return
	}
	if err := stream.Start(); err != nil {
		stream.Close()
		audioFile.Close()
		p.mu.Lock()
		p.errorMsg = "portaudio: " + err.Error()
		p.mu.Unlock()
		p.sendState(StateStopped)
		return
	}

	p.mu.Lock()
	p.audioFile = audioFile
	p.decoder = decoder
	p.paStream = stream
	p.paBuffer = paBuf
	p.sampleRate = sampleRate
	p.state = StatePlaying
	p.errorMsg = ""
	p.mu.Unlock()

	select {
	case p.fileChanged <- file:
	default:
	}

	err = p.pumpAudio()

	p.mu.Lock()
	p.closeDecoderLocked()
	advance := p.nextRequested
	p.nextRequested = false
	done := false
	if !advance {
		if (!p.shuffle && p.currentIdx+1 >= len(p.files)) ||
			(p.shuffle && len(p.done) >= len(p.files)) {
			done = true
		}
	}
	p.mu.Unlock()

	if err != nil && !advance {
		p.mu.Lock()
		p.errorMsg = "playback error: " + err.Error()
		p.mu.Unlock()
		p.sendState(StateStopped)
		return
	}

	if advance || done {
		if done {
			p.mu.Lock()
			p.state = StateStopped
			p.mu.Unlock()
			p.sendState(StateStopped)
		}
		return
	}

	p.Next()

	p.mu.Lock()
	if (!p.shuffle && p.currentIdx+1 >= len(p.files)) ||
		(p.shuffle && len(p.done) >= len(p.files)) {
		p.state = StateStopped
		p.mu.Unlock()
		p.sendState(StateStopped)
		return
	}
	p.mu.Unlock()
}

func (p *Player) pumpAudio() error {
	decoder := p.decoder
	stream := p.paStream
	paBuf := p.paBuffer
	sampleRate := p.sampleRate

	if decoder == nil || stream == nil || paBuf == nil {
		return nil
	}

	decoderBuf := make([]byte, 8192)
	var acc []int16
	var samples []float64

	flushEQ := func() {
		if len(samples) < fftSize {
			return
		}
		trimmed := samples
		if len(trimmed) > fftSize {
			trimmed = trimmed[len(trimmed)-fftSize:]
		}
		bars := computeEqualizerBars(trimmed, sampleRate)

		p.mu.Lock()
		p.eqData = bars
		p.mu.Unlock()

		select {
		case p.eqChan <- bars:
		default:
		}

		samples = make([]float64, 0, fftSize*2)
	}

	for {
		p.mu.RLock()
		state := p.state
		p.mu.RUnlock()

		switch state {
		case StateStopped:
			return nil
		case StatePaused:
			time.Sleep(50 * time.Millisecond)
			continue
		}

		n, err := decoder.Read(decoderBuf)
		if n > 0 {
			for i := 0; i < n/2; i++ {
				s := int16(uint16(decoderBuf[2*i]) | uint16(decoderBuf[2*i+1])<<8)
				acc = append(acc, s)
				samples = append(samples, float64(s)/32768.0)
			}

			for len(acc) >= len(paBuf) {
				if state == StateMuted {
					for i := range paBuf {
						paBuf[i] = 0
					}
				} else {
					copy(paBuf, acc[:len(paBuf)])
				}
				acc = acc[len(paBuf):]

				if writeErr := stream.Write(); writeErr != nil {
					return writeErr
				}
			}

			flushEQ()
		}

		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
	}

	if len(acc) > 0 {
		copy(paBuf, acc)
		for i := len(acc); i < len(paBuf); i++ {
			paBuf[i] = 0
		}
		stream.Write()
	}

	flushEQ()
	time.Sleep(100 * time.Millisecond)
	return nil
}

func (p *Player) updateEQ() {
}

func (p *Player) PlayCurrent() {
	select {
	case p.playNext <- struct{}{}:
	default:
	}
}

func (p *Player) Next() {
	if p.shuffle {
		p.mu.Lock()
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
		p.mu.Unlock()
	} else {
		p.mu.Lock()
		if p.currentIdx+1 >= len(p.files) {
			p.mu.Unlock()
			return
		}
		p.currentIdx++
		p.mu.Unlock()
	}

	select {
	case p.playNext <- struct{}{}:
	default:
	}
}

func convertToFloat64(data []byte) []float64 {
	out := make([]float64, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		sample := int16(uint16(data[i]) | uint16(data[i+1])<<8)
		out[i/2] = float64(sample) / 32768.0
	}
	return out
}

func applyHannWindow(samples []float64) []float64 {
	n := len(samples)
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		window := 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
		out[i] = samples[i] * window
	}
	return out
}

func isPowerOfTwo(n int) bool {
	return n > 0 && n&(n-1) == 0
}

func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 2
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

func computeEqualizerBars(samples []float64, sampleRate int) [numEqualizerBars]float64 {
	var bars [numEqualizerBars]float64
	if len(samples) == 0 {
		return bars
	}

	fftLen := nextPowerOfTwo(len(samples))
	if fftLen < 4 {
		fftLen = 4
	}

	windowed := applyHannWindow(samples)

	padded := make([]float64, fftLen)
	copy(padded, windowed)

	mags := computeFFTMagnitudes(padded)
	if mags == nil {
		return bars
	}

	binFreq := float64(sampleRate) / float64(fftLen)

	for i := range bars {
		loFreq := 20.0 * math.Pow(2, float64(i)*2.0/3.0)
		hiFreq := 20.0 * math.Pow(2, float64(i+1)*2.0/3.0)

		loBin := int(math.Ceil(loFreq / binFreq))
		hiBin := int(math.Floor(hiFreq / binFreq))
		if loBin < 0 {
			loBin = 0
		}
		if loBin >= len(mags) {
			continue
		}
		if hiBin >= len(mags) {
			hiBin = len(mags) - 1
		}
		if loBin > hiBin {
			continue
		}

		var sum float64
		for b := loBin; b <= hiBin; b++ {
			sum += mags[b]
		}
		count := hiBin - loBin + 1
		if count > 0 {
			bars[i] = sum / float64(count)
		}
	}

	maxVal := 0.0
	for _, v := range bars {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal > 0 {
		for i := range bars {
			bars[i] = math.Sqrt(bars[i] / maxVal)
			if bars[i] > 1 {
				bars[i] = 1
			}
		}
	}

	return bars
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
