package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/faiface/beep"
	gomp3 "github.com/hajimehoshi/go-mp3"
)

const (
	userAgent       = "muzak321"
	maxRedirects    = 10
	streamHandshake = 15 * time.Second
)

// isStreamURL reports whether path is a remote http(s) stream URL.
func isStreamURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

// streamNameFromURL returns a human friendly name for a stream URL: the last
// path segment when present, otherwise the host.
func streamNameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := u.Hostname()
	if host == "" {
		return rawURL
	}
	seg := strings.Trim(u.Path, "/")
	if i := strings.LastIndexByte(seg, '/'); i >= 0 {
		seg = seg[i+1:]
	}
	if seg == "" {
		return host
	}
	return host + "/" + seg
}

// icyReader wraps a SHOUTcast/Icecast response body, stripping the in-band ICY
// metadata blocks and exposing the latest StreamTitle. Only clean audio bytes
// are handed to the caller so an MP3 decoder never sees metadata frames.
type icyReader struct {
	r       io.Reader
	metaInt int64
	remain  int64

	mu      sync.Mutex
	title   string
	signals chan struct{} // buffered 1; pinged when the title changes
}

func newICYReader(r io.Reader, metaInt int64) *icyReader {
	return &icyReader{r: r, metaInt: metaInt, remain: metaInt, signals: make(chan struct{}, 1)}
}

func (r *icyReader) Title() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.title
}

// TitleChanged returns a channel that is signalled whenever StreamTitle changes.
func (r *icyReader) TitleChanged() <-chan struct{} {
	return r.signals
}

func (r *icyReader) Read(p []byte) (int, error) {
	if r.metaInt <= 0 {
		return r.r.Read(p)
	}

	if r.remain == 0 {
		if err := r.readMetadata(); err != nil {
			return 0, err
		}
		r.remain = r.metaInt
	}
	if int64(len(p)) > r.remain {
		p = p[:r.remain]
	}
	n, err := r.r.Read(p)
	r.remain -= int64(n)
	return n, err
}

func (r *icyReader) readMetadata() error {
	var lenByte [1]byte
	if _, err := io.ReadFull(r.r, lenByte[:]); err != nil {
		return err
	}
	metaLen := int(lenByte[0]) * 16
	if metaLen == 0 {
		return nil
	}
	buf := make([]byte, metaLen)
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return err
	}
	r.parse(buf)
	return nil
}

func (r *icyReader) parse(buf []byte) {
	s := string(buf)
	title := ""
	if i := strings.Index(s, "StreamTitle='"); i >= 0 {
		rest := s[i+len("StreamTitle='"):]
		if j := strings.Index(rest, "'"); j >= 0 {
			title = rest[:j]
		}
	}
	if title == "" {
		return
	}

	r.mu.Lock()
	changed := title != r.title
	r.title = title
	r.mu.Unlock()
	if changed {
		select {
		case r.signals <- struct{}{}:
		default:
		}
	}
}

// Stream represents an open live HTTP MP3 stream. It exposes the current
// StreamTitle and provides clean audio (ICY metadata stripped) via Read.
type Stream struct {
	body io.ReadCloser
	meta *icyReader
}

// Title returns the most recent StreamTitle ("" when the server sends none).
func (s *Stream) Title() string { return s.meta.Title() }

// TitleChanged returns a channel signalled when the live title updates.
func (s *Stream) TitleChanged() <-chan struct{} { return s.meta.TitleChanged() }

// Read forwards clean audio; ICY metadata has already been removed.
func (s *Stream) Read(p []byte) (int, error) { return s.meta.Read(p) }

func (s *Stream) Close() error { return s.body.Close() }

// openStream performs the GET and returns an open Stream. It waits up to
// ResponseHeaderTimeout for response headers but does not cap the body.
func openStream(rawURL string) (*Stream, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid stream URL: %w", err)
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: streamHandshake,
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			req.Header.Set("Icy-MetaData", "1")
			req.Header.Set("User-Agent", userAgent)
			return nil
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid stream URL: %w", err)
	}
	req.Header.Set("Icy-MetaData", "1")
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stream request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("stream returned status %s", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "audio/mpeg") {
		resp.Body.Close()
		return nil, fmt.Errorf("stream is not MP3 audio (Content-Type: %s)", ct)
	}

	metaInt, _ := strconv.ParseInt(resp.Header.Get("icy-metaint"), 10, 64)
	if metaInt < 0 {
		metaInt = 0
	}
	return &Stream{
		body: resp.Body,
		meta: newICYReader(resp.Body, metaInt),
	}, nil
}

// mp3Stream wraps a go-mp3 decoder as a non-seekable beep.Streamer. It always
// outputs stereo frames and counts decoded samples for elapsed-time display.
type mp3Stream struct {
	dec        *gomp3.Decoder
	sampleRate beep.SampleRate
	samples    int64
	err        error
	mu         sync.Mutex
}

func newMP3Stream(rd io.Reader) (*mp3Stream, error) {
	dec, err := gomp3.NewDecoder(rd)
	if err != nil {
		return nil, err
	}
	return &mp3Stream{
		dec:        dec,
		sampleRate: beep.SampleRate(dec.SampleRate()),
	}, nil
}

func (m *mp3Stream) SampleRate() beep.SampleRate { return m.sampleRate }

func (m *mp3Stream) Stream(samples [][2]float64) (int, bool) {
	var frame [4]byte
	n := 0
	for i := range samples {
		dn, err := m.dec.Read(frame[:])
		if dn == len(frame) {
			l := int16(uint16(frame[0]) | uint16(frame[1])<<8)
			r := int16(uint16(frame[2]) | uint16(frame[3])<<8)
			samples[i][0] = float64(l) / 32768.0
			samples[i][1] = float64(r) / 32768.0
			n++
			m.mu.Lock()
			m.samples++
			m.mu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				m.mu.Lock()
				m.err = err
				m.mu.Unlock()
			}
			break
		}
	}
	return n, n > 0
}

func (m *mp3Stream) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

// PlayedSamples returns how many stereo sample frames have been decoded.
func (m *mp3Stream) PlayedSamples() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.samples
}

func (m *mp3Stream) Close() error { return nil }
