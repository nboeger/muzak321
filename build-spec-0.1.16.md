# AI Build Specification: muzak321 0.1.16 — Player Polish

> **Purpose:** This spec is for an AI developer agent (pi) to implement the
> player-polish feature set for muzak321: spectrum equalizer, seeking,
> terminal cover art, synced .lrc lyrics, playlist save/reload + recently
> played, and track-change notifications. It defines concrete designs,
> constants, file targets, keybindings, and acceptance tests.
>
> **Baseline:** repo `nboeger/muzak321`, tag `0.1.15` — `go build` + `go test ./...`
> green. Do not regress: **every phase must end with build + tests green.**
>
> **Build command (local Go via gvm — Go 1.25.0 at `$HOME/.gvm/go/bin/go`;**
> **requires gcc + libasound2-dev for the CGO/ALSA path):**
> ```bash
> export PATH="$HOME/.gvm/go/bin:$PATH"
> go vet ./... && go build -o /dev/null . && go test ./...
> ```
> Docker fallback if Go is unavailable:
> ```bash
> docker run --rm -v "$(pwd):/work" -w /work golang:1.25-bookworm \
>   sh -c "apt-get update -qq && apt-get install -y -qq gcc libasound2-dev >/dev/null && \
>          go vet ./... && go build -buildvcs=false -o /dev/null . && go test ./... "
> ```
>
> **Language:** Go 1.25 (go.mod). **Target:** Linux TUI (tview/tcell). No new
> runtime dependencies unless a Part explicitly allows one. Keep the
> "single binary, no runtime deps" property.

---

## Scope (exactly this)

- **Part A:** Real FFT spectrum equalizer (animated bars while playing)
- **Part B:** Seeking (relative/absolute, clamped)
- **Part C:** Cover art rendered as truecolor half-blocks
- **Part D:** Synced .lrc lyrics
- **Part E:** Playlist save/reload + recently-played history
- **Part F:** Track-change desktop notifications (notify-send)
- **Part G:** MPRIS media-key integration (STRETCH — skip if the first six
  parts are not all green; it is optional)

**Out of scope:** Windows/BSD (audio backend has no drivers there), gapless
playback, replaygain, audio effects/EQ filtering (visualizer only), tag
editing, network lyrics lookup.

---

## Part A — Spectrum Equalizer

### Goal
Animated frequency bars in the player view, driven by a real FFT of the
audio currently playing. Bars respond to actual signal content (a 440 Hz
tone must light a different band than a 1 kHz tone). Not a fake animation.

### Design

**Sample tap (player.go).** `volStreamer.Stream` (player.go) is the single
choke point every decoded frame passes through. Add a tap there:

```go
type volStreamer struct {
    mu     sync.Mutex
    inner  beep.Streamer
    volume float64
    tap    *sampleTap   // nil when disabled; set on the player's streamers
}
```

In `Stream`, copy the RAW samples (before the volume multiply) into the
tap: mono-mix `(samples[i][0]+samples[i][1])/2`. The tap must be a no-op
when nil, and must never block or slow the audio path beyond a memcpy.

**Ring buffer (new file `spectrum.go`).**

```go
// sampleTap keeps the most recent window of mono samples.
type sampleTap struct {
    mu   sync.Mutex
    buf  []float64
    pos  int // next write index (ring)
    full bool
}
func newSampleTap(size int) *sampleTap
func (t *sampleTap) write(samples []float64)          // called from speaker goroutine
func (t *sampleTap) window(n int) []float64           // newest n samples, oldest first (copy)
```

- `SpectrumWindow = 4096` (power of two, ~93 ms at 44.1 kHz).
- `Player` owns one tap, created in `NewPlayer`, attached to the
  `volStreamer` built in `playCurrent`.
- Thread safety: the speaker goroutine writes, the UI goroutine reads;
  a `sync.Mutex` on the tap is sufficient (critical sections are tiny).

**FFT + bands (spectrum.go).** Hand-rolled iterative radix-2 Cooley–Tukey
FFT over the Hann-windowed 4096 samples. No new dependency; if the
implementation proves flaky, `github.com/mjibson/go-dsp/fft` is an
acceptable fallback (document the choice in a comment).

```go
// spectrumBands returns len(bandEdges)-1 normalized magnitudes in [0,1].
// Log-spaced bands from 40 Hz to 16 kHz. Magnitudes are normalized per
// window (sum of bin magnitudes), scaled by 20*log10, and clamped to [0,1]
// with a small floor (silence maps to ~0).
func spectrumBands(samples []float64, bandEdges []float64, sampleRate int) []float64
```

Constants:
- `SpectrumBands = 14`, `SpectrumBandMinHz = 40`, `SpectrumBandMaxHz = 16000`
- Band edges: `f[i] = minHz * (maxHz/minHz)^(i/N)` for i in 0..N (log spacing);
  a bin of width `sampleRate/SpectrumWindow` maps to its band by center frequency.
- Mono mixing, Hann window applied before FFT.

**Smoothing (Player state, player.go).** Keep the previous frame; blend:
`v = attack*new + (1-attack)*old` when rising, `v = decay*new + (1-decay)*old`
when falling. Constants `SpectrumAttack = 0.6`, `SpectrumDecay = 0.85`.

```go
func (p *Player) Spectrum() []float64 // smoothed 14-band frame; empty when stopped
```

**UI (ui.go).** New `spectrum *tview.TextView` (dynamic colors, black bg).
Player page layout becomes (insert between progress and playlist):

```go
playerPage := tview.NewFlex().SetDirection(tview.FlexRow).
    AddItem(header, 1, 0, false).
    AddItem(u.progress, 1, 0, false).
    AddItem(u.spectrum, 3, 0, false).      // NEW: 3 rows
    AddItem(u.playlist, 0, 1, false).
    AddItem(status, 1, 0, false)
```

`SetSpectrum(values []float64, active bool)`: render one column per band
using block characters `▁▂▃▄▅▆▇█` (8 levels) across the 3 rows (24 levels
total per band), colored with a green→yellow→red truecolor gradient based
on value, e.g. `[#00ff00]█[-]` / `[#ffff00]` / `[#ff0000]`. When `active`
is false, render a dimmed flat baseline. When values is empty (stopped),
clear the view.

**Ticker (main.go).** Add `pumpSpectrum` alongside `pumpProgress`, at
`33 * time.Millisecond` (~30 fps), guarded by `a.page == "player"` and
`a.player.State() == StatePlaying`:

```go
go a.pumpSpectrum()

func (a *App) pumpSpectrum() {
    ticker := time.NewTicker(33 * time.Millisecond)
    defer ticker.Stop()
    for range ticker.C {
        a.ui.Queue(func() {
            if a.page != "player" { return }
            active := a.player.State() == StatePlaying
            a.ui.SetSpectrum(a.player.Spectrum(), active)
        })
    }
}
```

### Acceptance
- A 440 Hz sine test tone lights bands around the 440 Hz region; a 1 kHz
  tone lights a different, higher region (unit test, tolerance ±1 band).
- Silence produces all-zero bands; DC produces a band-0-heavy frame.
- Ring buffer returns the newest N samples in chronological order; wraps
  correctly past capacity.
- Attack/decay smoothing: a step input converges within ~10 frames; a
  dropped signal decays to ~0 within ~30 frames.
- UI row renders 14 columns × 3 rows of block glyphs with truecolor codes
  when active; clears when stopped.
- All pre-existing tests (shuffle, playargs, advance) stay green.

---

## Part B — Seeking

### Goal
Seek within the current track: 5 s steps with ←/→, 30 s with Shift+←/→,
start/end with Home/End. Not supported on live streams (error, no crash).

### Design (player.go)
```go
// SeekTo seeks to an absolute position, clamped to [0, duration].
// Returns an error on live streams (curMP3Stream != nil) or when no
// decoder is open.
func (p *Player) SeekTo(pos time.Duration) error
// SeekRelative seeks by ±delta (negative allowed), clamped.
func (p *Player) SeekRelative(delta time.Duration) error
```

Implementation: `p.streamer` is a `beep.StreamSeekCloser` for decoded
files (mp3/flac/ogg/wav all satisfy it via beep). Convert position to
samples (`sr.N(pos)`), clamp to `[0, p.streamer.Len()]`, call
`p.streamer.Seek(n)`. No restart of playback needed — the speaker keeps
pulling from the new position. Emit a UI refresh via `fileChanged` so the
progress bar jumps immediately.

Constants: `SeekStep = 5 * time.Second`, `SeekStepLarge = 30 * time.Second`.

### Keybindings (main.go `playerKey`)
- `←` / `→` — seek ∓5 s; `Shift+←` / `Shift+→` — ∓30 s
- `Home` — seek to 0; `End` — seek to duration
- Update `printHelp()` and the in-app help page.

### Acceptance
- Seek clamps: negative seeks clamp to 0; beyond-end seeks clamp to
  duration; `Progress()` reports the seeked position within 100 ms.
- Seeking a live stream returns a clear error and does not disturb
  playback or the stream connection.
- Seek works mid-playback and while paused.

---

## Part C — Cover Art (truecolor half-blocks)

### Goal
Show embedded album art from the current file as a truecolor half-block
rendering beside the playlist — no external tools, no new dependencies.

### Design

**Extraction (player.go).** `dhowden/tag` (already a dependency, used for
Title/Artist) exposes `metadata.Picture()` → `(pic metadata.Picture, err)`
with `pic.Data []byte` (JPEG or PNG). In `playCurrent`, for the file (not
stream) path, read tags and stash the picture bytes + format on the
Player:

```go
// CoverArt returns the current file's embedded art bytes + MIME, or nil.
func (p *Player) CoverArt() ([]byte, string)
```

**Rendering (new file `coverart.go`).** Pure function, unit-testable:

```go
// coverArtBlock renders image data as width×height half-block ANSI
// truecolor rows. Each output cell is one "▀" glyph: foreground = upper
// pixel, background = lower pixel. Returns "" on decode failure.
func coverArtBlock(data []byte, width, height int) string
```

- Decode with stdlib `image/jpeg` / `image/png` (sniff magic bytes).
- Scale to `CoverArtWidth = 24` cells × `CoverArtHeight = 12` cells
  (= 48×24 pixels) with nearest-neighbor or box sampling.
- Truecolor codes use tview dynamic-color syntax (v0.42+ uses
  COLON-separated tags): `[#RRGGBB:#RRGGBB]▀[-]` — NOT the old
  comma form `[fg,bg]`, which renders literally. Single-color tags
  like `[#00ff00]` are fine.
- Handle odd heights (pad last row with black).

**UI (ui.go).** Restructure the player page into a column: the existing
row stack on the left, a fixed-width `coverArt *tview.TextView` (24 cells)
on the right:

```go
playerPage := tview.NewFlex().SetDirection(tview.FlexColumn).
    AddItem(leftStack, 0, 1, false).
    AddItem(u.coverArt, 24, 0, false)
```

`SetCoverArt(data []byte, mime string)`: re-render via `coverArtBlock`;
empty input clears the pane. Called from `renderPlayer` (main.go) on
track change; cache the rendered string so it is not re-rendered every
100 ms tick.

### Acceptance
- A generated 48×24 PNG (stdlib `image/png` in the test) renders to a
  24×12 string of `▀` cells containing truecolor codes; each cell's fg/bg
  match the sampled pixels.
- Garbage bytes → empty string (no panic). Empty/no-art → cleared pane.
- Odd dimensions and 1×1 images do not panic.

---

## Part D — Synced .lrc Lyrics

### Goal
Show synced lyrics for the current track, toggled with `l`, auto-scrolling
with playback position. Offline only: a `.lrc` file next to the audio
file (`<basename>.lrc`).

### Design

**Parser (new file `lyrics.go`).**
```go
type LyricLine struct {
    At   time.Duration
    Text string
}
// parseLRC parses timestamped [mm:ss.xx] / [mm:ss] lines, ignoring
// metadata tags ([ti:], [ar:], [offset:], ...). Multiple timestamps on
// one line produce one LyricLine each. Malformed lines are skipped.
func parseLRC(data []byte) []LyricLine
// loadLRC returns parsed lyrics for a track path, or an error when no
// sibling .lrc exists.
func loadLRC(trackPath string) ([]LyricLine, error)
// currentLyric returns the index of the last line with At <= pos, or -1.
func currentLyric(lines []LyricLine, pos time.Duration) int
```

**Player (player.go).** Cache `lyrics []LyricLine` + the file path they
were loaded from (cleared on track change). Expose
`Lyrics() []LyricLine` and `CurrentLyricIndex() int` (uses `Progress()`).

**UI (ui.go / main.go).**
- New page `"lyrics"`: a bordered TextView. `l` in player view toggles
  between the playlist stack and the lyrics page (back via `l` or `Esc`).
- While the lyrics page is visible, a `pumpLyrics`-style refresh (reuse
  `pumpProgress`, it already ticks 10×/s): highlight the current line
  (`[::b]` / color), keep it visible with `SetScrollTo` when it changes.
- No `.lrc` file → `l` shows "No lyrics found for this track" and stays
  on the player page.

### Acceptance
- `parseLRC`: 3 timestamps + metadata tags + malformed lines parse to the
  expected LyricLines; `[mm:ss.xx]` and `[mm:ss]` both accepted.
- `currentLyric` monotonic: before first → -1, between lines → previous
  index, after last → last index.
- Toggle: `l` switches pages and back; no `.lrc` case shows the notice
  without leaving the player page.

---

## Part E — Playlist Save/Reload + Recently Played

### Goal
Save the current queue to a fixed m3u (`s`), reload it (`S`), and keep a
recently-played history (`y`) you can jump back into.

### Design

**Storage (new file `library.go`).**
- Data dir: `~/.muzak321/` (0700, created on first use; override with
  env `MUZAK321_DATA_DIR`).
- `SavePlaylist(path string, files []string) error` — writes a plain m3u
  (`#EXTM3U` header, one absolute path per line) — must round-trip through
  the existing `parseM3U` (playlist.go).
- `AppendHistory(trackPath string) error` — append `unix-ts\tpath` to
  `history.log`; dedupe consecutive repeats; cap at `HistoryMax = 200`
  entries (trim oldest, rewrite).
- `LoadHistory() [][]string` — most-recent-first `[ts, path]` pairs.

**Player hooks (player.go).** In `playCurrent`, after a successful decode
of a file (not a stream), call `AppendHistory(file)` fire-and-forget
(errors logged to the status bar only).

**Keybindings (main.go `playerKey`).**
- `s` — save queue to `~/.muzak321/last.m3u`; status shows
  "Saved N tracks".
- `S` — load `last.m3u` and play it (`SetFiles` + play, respecting the
  current shuffle flag).
- `y` — new `"history"` page: bordered list, most-recent-first; `Enter`
  plays that file (`SetFiles([path])` + play); `Esc`/`y` returns.
- Update help text.

### Acceptance
- Save→`parseM3U` round-trip returns the same file list.
- History appends, dedupes consecutive repeats, and trims to 200.
- `S` reloads and plays; `y` lists history and `Enter` plays a file.
- Streaming URLs are never written to history.

---

## Part F — Track-Change Notifications

### Goal
A desktop notification on each new track (decoded files only), using
`notify-send` when available. Disabled with `MUZAK321_NO_NOTIFY=1`.

### Design

**Notifier (new file `notify.go`).**
```go
// notifyArgs builds the notify-send argv for a track (title, artist from
// the existing tag read, else the file name).
func notifyArgs(title, artist, file string) []string
// notify fires notify-send in a detached goroutine; all errors are
// swallowed (best-effort by design).
func notify(title, artist, file string)
```

- `notify` checks `os.Getenv("MUZAK321_NO_NOTIFY") == ""` and
  `exec.LookPath("notify-send")`; then
  `exec.Command("notify-send", "-a", "muzak321", args...).Start()`.
- Called from `playCurrent` (file path only, after decode success).

### Acceptance
- `notifyArgs` returns `["muzak321", "Title — Artist"]` style argv
  (exact shape per the notify-send contract: summary "muzak321", body
  "Title — Artist" or the file name when no tags).
- `MUZAK321_NO_NOTIFY=1` disables; missing `notify-send` is a silent
  no-op.
- Notifications never delay or crash playback (fire-and-forget).

---

## Part G — MPRIS Media Keys (STRETCH, optional)

Only if Parts A–F are fully green and the agent has budget. Otherwise
skip entirely — it is explicitly optional.

- Dependency allowed: `github.com/godbus/dbus/v5`.
- Expose `org.mpris.MediaPlayer2.muzak321`:
  - Properties: `PlaybackStatus`, `CanGoNext/CanGoPrevious/CanPlay/CanPause`,
    `CanSeek=false` (v1), `Metadata` (xesam:title/artist/url),
    `Position` (µs, computed from `Progress()`).
  - Methods: `Play`, `Pause`, `PlayPause`, `Next`, `Previous`, `Stop`.
- Serve in a goroutine started by `main()`; ignore DBus errors.
- **Acceptance:** a `dbus-send`/`playerctl play-pause` call toggles
  playback; `Next`/`Previous` advance the queue; no crash when no
  session bus exists (headless).

---

## Build Order & Commit Rules

1. Part A (spectrum) — largest; land it alone.
2. Part B (seek).
3. Part C (cover art).
4. Part D (lyrics).
5. Part E (library: playlists + history).
6. Part F (notifications).
7. Part G (MPRIS) — only if all previous green and budget remains.

Commit per part (`feat: spectrum equalizer`, `feat: seeking`, ...). Every
commit: `go vet ./...` clean, `go build` clean, `go test ./...` green,
plus the part's new tests. Bump nothing yet (version stays 0.1.15 until
the release tag).

## Keybinding Summary (player view)

| Key | Action | Part |
|---|---|---|
| `←` / `→` | Seek ∓5 s | B |
| `Shift+←` / `Shift+→` | Seek ∓30 s | B |
| `Home` / `End` | Seek start / end | B |
| `l` | Toggle lyrics | D |
| `s` | Save playlist (last.m3u) | E |
| `S` | Reload + play last.m3u | E |
| `y` | Recently played | E |
| _(media keys)_ | Play/pause/next/prev | G |

Existing keys (Space, M, P/N, Up/Down, A, Q, H) are unchanged.

## Release Checklist (after all parts green)

1. Bump `snap/snapcraft.yaml` `version:` to `0.1.16` in the same commit
   as the tag (the snap workflow guard fails otherwise).
2. Tag `0.1.16` and push — GoReleaser + snap run automatically.
3. After the release, refresh the Homebrew tap formula sha256
   (the retag lesson: always re-hash the source archive).
4. Optional: fill the Nix `vendorHash` via a local build.
