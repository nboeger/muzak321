package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/hajimehoshi/go-mp3"
)

const (
	fftSize         = 1024
	numEqualizerBars = 16
)

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

	decoder     *mp3.Decoder
	audioFile   *os.File
	ffplayCmd   *exec.Cmd
	ffplayStdin io.WriteCloser

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
	if p.ffplayStdin != nil {
		p.ffplayStdin.Close()
	}
	if p.ffplayCmd != nil && p.ffplayCmd.Process != nil {
		p.ffplayCmd.Process.Kill()
	}
	if p.audioFile != nil {
		p.audioFile.Close()
	}
	p.decoder = nil
	p.audioFile = nil
	p.ffplayCmd = nil
	p.ffplayStdin = nil
	p.state = StateStopped
}

func (p *Player) playCurrent() {
	p.mu.Lock()
	if len(p.files) == 0 {
		p.state = StateStopped
		p.mu.Unlock()
		return
	}

	p.closeDecoderLocked()

	file := p.files[p.currentIdx]

	audioFile, err := os.Open(file)
	if err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		select {
		case p.stateChan <- StateStopped:
		default:
		}
		return
	}

	decoder, err := mp3.NewDecoder(audioFile)
	if err != nil {
		audioFile.Close()
		p.state = StateStopped
		p.mu.Unlock()
		select {
		case p.stateChan <- StateStopped:
		default:
		}
		return
	}

	sampleRate := decoder.SampleRate()

	ffplay := exec.Command("ffplay",
		"-nodisp",
		"-autoexit",
		"-loglevel", "quiet",
		"-f", "s16le",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-ac", "2",
		"-i", "pipe:0",
	)

	stdin, err := ffplay.StdinPipe()
	if err != nil {
		audioFile.Close()
		p.state = StateStopped
		p.mu.Unlock()
		select {
		case p.stateChan <- StateStopped:
		default:
		}
		return
	}

	err = ffplay.Start()
	if err != nil {
		stdin.Close()
		audioFile.Close()
		p.state = StateStopped
		p.mu.Unlock()
		select {
		case p.stateChan <- StateStopped:
		default:
		}
		return
	}

	p.audioFile = audioFile
	p.decoder = decoder
	p.ffplayCmd = ffplay
	p.ffplayStdin = stdin
	p.sampleRate = sampleRate
	p.state = StatePlaying
	p.mu.Unlock()

	select {
	case p.fileChanged <- file:
	default:
	}

	p.pumpAudio()
}

func (p *Player) pumpAudio() {
	decoder := p.decoder
	stdin := p.ffplayStdin
	sampleRate := p.sampleRate

	if decoder == nil || stdin == nil {
		return
	}

	defer func() {
		stdin.Close()
	}()

	buf := make([]byte, 8192)
	silenceBuf := make([]byte, 8192)
	var samples []float64
	done := false

	for !done {
		p.mu.RLock()
		state := p.state
		p.mu.RUnlock()

		switch state {
		case StateStopped:
			return
		case StatePaused:
			time.Sleep(50 * time.Millisecond)
			continue
		}

		n, err := decoder.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if state == StateMuted {
				chunk = silenceBuf[:n]
			}

			if _, writeErr := stdin.Write(chunk); writeErr != nil {
				return
			}

			samples = append(samples, convertToFloat64(buf[:n])...)
			if len(samples) >= fftSize*2 {
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
		}

		if err == io.EOF {
			done = true
		} else if err != nil {
			return
		}
	}

	time.Sleep(100 * time.Millisecond)
	p.Next()
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
