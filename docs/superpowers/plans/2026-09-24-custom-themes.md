# Custom Themes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a muzak321 user override the app's UI colors by dropping a plain-text `theme.conf` file in their XDG config directory, with no dependency and no change to today's look when the file is absent.

**Architecture:** All 16 themeable colors move into a new `theme.go` as package-level vars, defaulted to today's hardcoded values. A tiny stdlib `key = #rrggbb` parser (`applyThemeReader`) patches those vars from an `io.Reader`; `loadTheme()` wraps it with XDG path resolution and is called once in `main()` before the UI is built. `ui.go` and `spectrum.go` lose their local color declarations and reference the `theme.go` vars instead; `spectrumColor` is rewritten from a 2-branch formula to a 3-keyframe linear interpolation (`spectrum_low`/`spectrum_mid`/`spectrum_high`), which reproduces today's output exactly at the defaults.

**Tech Stack:** Go 1.25, stdlib only (`bufio`, `strconv`, `os`, `path/filepath`), `github.com/gdamore/tcell/v2` (already a dependency) for `tcell.Color`/`NewRGBColor`/`RGB()`.

**Spec:** `docs/superpowers/specs/2026-09-24-custom-themes-design.md`

## Global Constraints

- No new third-party dependencies — parsing is stdlib-only.
- Config path is fixed: `filepath.Join(os.UserConfigDir(), "muzak321", "theme.conf")`. No `--theme` flag, no alternate locations.
- Missing theme.conf: silent, no warning, built-in defaults used.
- Malformed line / unknown key / invalid color value: one warning line to stderr, that key keeps its default, startup always continues (never fatal, never exits non-zero because of theme.conf).
- `spectrumColor(v float64) string` keeps its exact existing signature and 6-hex-digit (no `#`) return format — callers in `spectrum.go` are unchanged.

## Review Focus

- Theme dir/file doesn't exist (the common case — most users never create one): `loadTheme()` must return silently and the app must start with today's exact default colors, no crash, no stderr output.
- A value line missing the leading `#` (e.g. `header_bg = 3a3a3a`) or with the wrong number of hex digits: must warn (not crash, not silently misapply), and that key keeps its default.
- Extra/irregular whitespace around `=`, the key, or the value (e.g. `header_bg   =   #112233`): must still parse correctly — a user hand-editing the file shouldn't get silently ignored due to spacing.
- An unknown/misspelled key (e.g. `boarder_playlist` instead of `border_playlist`): must warn and name the exact bad key, not fail silently with no feedback and not crash.
- A theme.conf that exists but contains only blank lines and/or comments: must produce zero warnings and leave every color at its default — an empty/comment-only file is not an error.

---

## Task 1: Theme engine core (theme.go)

**Files:**
- Create: `theme.go`
- Create: `theme_test.go`

**Interfaces:**
- Produces (consumed by Task 2 and Task 3):
  - `var colHeader, colPaleText, colError tcell.Color`
  - `var borderColorPlaylist, borderColorCoverArt, borderColorSpectrum tcell.Color`
  - `var spectrumLow, spectrumMid, spectrumHigh tcell.Color`
  - `var colBodyBG tcell.Color`
  - `var colPlaylistSelFG, colPlaylistSelBG, colBrowserSelFG, colBrowserSelBG tcell.Color`
  - `var colBarFill, colAmber, colTeal string` (pre-built tview markup tags)
  - `const colReset = "[-:-]"`
  - `func hexToColor(s string) (tcell.Color, error)` — parses `"#rrggbb"`.
  - `func colorHex(c tcell.Color) string` — inverse, returns 6 lowercase hex digits, no `#`.
  - `func loadTheme()` — resolves the XDG path and applies the file if present; no-op otherwise.

- [ ] **Step 1: Write failing tests for `hexToColor` and `colorHex`**

```go
// theme_test.go
package main

import (
	"strings"
	"testing"
)

func TestHexToColorValid(t *testing.T) {
	c, err := hexToColor("#3a9bd0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := colorHex(c); got != "3a9bd0" {
		t.Errorf("colorHex(hexToColor(%q)) = %q, want %q", "#3a9bd0", got, "3a9bd0")
	}
}

func TestHexToColorInvalid(t *testing.T) {
	cases := []string{
		"3a9bd0",   // missing '#'
		"#3a9bd",   // 5 digits
		"#3a9bd00", // 7 digits
		"#gggggg",  // non-hex digits
		"",
	}
	for _, s := range cases {
		if _, err := hexToColor(s); err == nil {
			t.Errorf("hexToColor(%q): want error, got nil", s)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./... -run 'TestHexToColor' -v`
Expected: FAIL — `hexToColor`/`colorHex` undefined (theme.go doesn't exist yet).

- [ ] **Step 3: Create theme.go with defaults, hexToColor, and colorHex**

```go
package main

import (
	"fmt"
	"strconv"

	"github.com/gdamore/tcell/v2"
)

// Themeable colors, exposed as package vars so loadTheme can override them
// at startup. Defaults reproduce the existing btop-style palette exactly.
var (
	colHeader   = mustColor("3a3a3a")
	colPaleText = mustColor("c0c0c0")
	colError    = mustColor("a86b6b")

	borderColorPlaylist = mustColor("6b9b9b")
	borderColorCoverArt = mustColor("a88bb5")
	borderColorSpectrum = mustColor("8fb08a")

	spectrumLow  = mustColor("5fb05f")
	spectrumMid  = mustColor("b0b05f")
	spectrumHigh = mustColor("b05f5f")

	colBodyBG = mustColor("000000")

	colPlaylistSelFG = mustColor("ffffff")
	colPlaylistSelBG = mustColor("000000")
	colBrowserSelFG  = mustColor("000000")
	colBrowserSelBG  = mustColor("ffffff")
)

// Text-tag colors: pre-built tview markup strings, rebuilt whenever their
// underlying hex changes (see themeSetters).
var (
	barFillBGHex = "2a2a2a"
	barFillFGHex = "6b9b8f"
	colBarFill   = buildBarFill()

	accentAmberHex = "c9b46b"
	colAmber       = "[#" + accentAmberHex + "]"

	accentTealHex = "6b9b9b"
	colTeal        = "[#" + accentTealHex + "]"
)

const colReset = "[-:-]"

func buildBarFill() string {
	return "[#" + barFillBGHex + ":" + barFillFGHex + "]"
}

// hexToColor parses a "#rrggbb" string into a tcell.Color.
func hexToColor(s string) (tcell.Color, error) {
	if len(s) != 7 || s[0] != '#' {
		return 0, fmt.Errorf("invalid color %q: want #rrggbb", s)
	}
	v, err := strconv.ParseInt(s[1:], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid color %q: %w", s, err)
	}
	r := int32((v >> 16) & 0xff)
	g := int32((v >> 8) & 0xff)
	b := int32(v & 0xff)
	return tcell.NewRGBColor(r, g, b), nil
}

// colorHex returns the 6-digit lowercase hex string for a tcell.Color, with
// no leading '#'.
func colorHex(c tcell.Color) string {
	r, g, b := c.RGB()
	return fmt.Sprintf("%02x%02x%02x", r, g, b)
}

// mustColor parses a 6-digit hex string (no leading '#') into a tcell.Color.
// It panics on invalid input; only used for the compile-time-correct
// default palette above.
func mustColor(hex string) tcell.Color {
	c, err := hexToColor("#" + hex)
	if err != nil {
		panic(err)
	}
	return c
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./... -run 'TestHexToColor' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add theme.go theme_test.go
git commit -m "feat: add theme color parsing (hexToColor/colorHex)"
```

- [ ] **Step 6: Write failing tests for `applyThemeReader`**

```go
// append to theme_test.go

func withColHeader(t *testing.T, f func()) {
	t.Helper()
	orig := colHeader
	t.Cleanup(func() { colHeader = orig })
	f()
}

func TestApplyThemeReaderValidLine(t *testing.T) {
	withColHeader(t, func() {
		warnings := applyThemeReader(strings.NewReader("header_bg = #112233\n"))
		if len(warnings) != 0 {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
		if got := colorHex(colHeader); got != "112233" {
			t.Errorf("colHeader = %q, want %q", got, "112233")
		}
	})
}

func TestApplyThemeReaderCommentsAndBlanks(t *testing.T) {
	withColHeader(t, func() {
		before := colHeader
		warnings := applyThemeReader(strings.NewReader("\n  \n# a comment\n   # indented comment\n"))
		if len(warnings) != 0 {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
		if colHeader != before {
			t.Errorf("colHeader changed on a comment/blank-only file")
		}
	})
}

func TestApplyThemeReaderWhitespaceTolerant(t *testing.T) {
	withColHeader(t, func() {
		warnings := applyThemeReader(strings.NewReader("   header_bg   =   #445566   \n"))
		if len(warnings) != 0 {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
		if got := colorHex(colHeader); got != "445566" {
			t.Errorf("colHeader = %q, want %q", got, "445566")
		}
	})
}

func TestApplyThemeReaderMalformedLine(t *testing.T) {
	warnings := applyThemeReader(strings.NewReader("not a valid line at all\n"))
	if len(warnings) != 1 || !strings.Contains(warnings[0], "line 1") {
		t.Errorf("warnings = %v, want one warning mentioning line 1", warnings)
	}
}

func TestApplyThemeReaderUnknownKey(t *testing.T) {
	warnings := applyThemeReader(strings.NewReader("boarder_playlist = #112233\n"))
	if len(warnings) != 1 || !strings.Contains(warnings[0], "boarder_playlist") {
		t.Errorf("warnings = %v, want one warning naming the bad key", warnings)
	}
}

func TestApplyThemeReaderMissingHash(t *testing.T) {
	withColHeader(t, func() {
		before := colHeader
		warnings := applyThemeReader(strings.NewReader("header_bg = 3a3a3a\n"))
		if len(warnings) != 1 {
			t.Fatalf("warnings = %v, want exactly one warning", warnings)
		}
		if colHeader != before {
			t.Errorf("colHeader changed despite an invalid value")
		}
	})
}
```

- [ ] **Step 7: Run to verify it fails**

Run: `go test ./... -run 'TestApplyThemeReader' -v`
Expected: FAIL — `applyThemeReader` undefined.

- [ ] **Step 8: Implement `applyThemeReader` and `themeSetters`**

Update the `theme.go` import block from Step 3 to include the additional
stdlib packages this step needs:

```go
import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)
```

Then append to theme.go:

```go
// themeSetters maps each theme.conf key to the function that applies a
// validated 6-digit hex value (no leading '#') to the matching package
// var(s).
var themeSetters = map[string]func(hex string){
	"header_bg": func(hex string) { colHeader = mustColor(hex) },
	"header_fg": func(hex string) { colPaleText = mustColor(hex) },
	"error_bg":  func(hex string) { colError = mustColor(hex) },

	"bar_fill_bg": func(hex string) { barFillBGHex = hex; colBarFill = buildBarFill() },
	"bar_fill_fg": func(hex string) { barFillFGHex = hex; colBarFill = buildBarFill() },

	"accent_amber": func(hex string) { accentAmberHex = hex; colAmber = "[#" + hex + "]" },
	"accent_teal":  func(hex string) { accentTealHex = hex; colTeal = "[#" + hex + "]" },

	"border_playlist": func(hex string) { borderColorPlaylist = mustColor(hex) },
	"border_coverart": func(hex string) { borderColorCoverArt = mustColor(hex) },
	"border_spectrum": func(hex string) { borderColorSpectrum = mustColor(hex) },

	"spectrum_low":  func(hex string) { spectrumLow = mustColor(hex) },
	"spectrum_mid":  func(hex string) { spectrumMid = mustColor(hex) },
	"spectrum_high": func(hex string) { spectrumHigh = mustColor(hex) },

	"body_bg": func(hex string) { colBodyBG = mustColor(hex) },

	"playlist_selected_fg": func(hex string) { colPlaylistSelFG = mustColor(hex) },
	"playlist_selected_bg": func(hex string) { colPlaylistSelBG = mustColor(hex) },
	"browser_selected_fg":  func(hex string) { colBrowserSelFG = mustColor(hex) },
	"browser_selected_bg":  func(hex string) { colBrowserSelBG = mustColor(hex) },
}

// applyThemeReader parses theme.conf-formatted content from r, applying
// each valid "key = #rrggbb" line to the matching package var via
// themeSetters. It returns one warning string per malformed line, unknown
// key, or invalid color value; valid lines, blank lines, and full-line
// comments (first non-whitespace char '#') produce no warning.
func applyThemeReader(r io.Reader) []string {
	var warnings []string
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			warnings = append(warnings, fmt.Sprintf("line %d: malformed line %q (want key = #rrggbb)", lineNo, line))
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		setter, ok := themeSetters[key]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("line %d: unknown key %q", lineNo, key))
			continue
		}
		if _, err := hexToColor(value); err != nil {
			warnings = append(warnings, fmt.Sprintf("line %d: %v", lineNo, err))
			continue
		}
		setter(value[1:]) // strip leading '#'
	}
	return warnings
}

// loadTheme applies $XDG_CONFIG_HOME/muzak321/theme.conf (falling back to
// ~/.config per os.UserConfigDir) over the default palette, if present.
// A missing file or directory is silent; malformed content produces
// warnings on stderr but never stops startup.
func loadTheme() {
	dir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	f, err := os.Open(filepath.Join(dir, "muzak321", "theme.conf"))
	if err != nil {
		return
	}
	defer f.Close()
	for _, w := range applyThemeReader(f) {
		fmt.Fprintln(os.Stderr, "warning: theme.conf "+w)
	}
}
```

- [ ] **Step 9: Run to verify it passes**

Run: `go test ./... -run 'TestApplyThemeReader' -v`
Expected: PASS

- [ ] **Step 10: Write and run tests for `loadTheme` end-to-end**

Add `"os"` to `theme_test.go`'s import block, then append:

```go
func TestLoadThemeMissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	before := colHeader
	loadTheme() // must not panic; no theme.conf exists under the temp dir
	if colHeader != before {
		t.Errorf("colHeader changed despite no theme.conf existing")
	}
}

func TestLoadThemeAppliesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(dir+"/muzak321", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/muzak321/theme.conf", []byte("header_bg = #654321\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := colHeader
	t.Cleanup(func() { colHeader = orig })
	loadTheme()
	if got := colorHex(colHeader); got != "654321" {
		t.Errorf("colHeader = %q, want %q", got, "654321")
	}
}
```

Run: `go test ./... -run 'TestLoadTheme' -v`
Expected: PASS for both (this pins the real XDG-path + file-read round trip, on top of the pure-parser coverage from Step 6).

- [ ] **Step 11: Run the full theme test file and `go vet`**

Run: `go test ./... -run 'Test.*Theme|TestHexToColor' -v && go vet ./...`
Expected: all PASS, no vet issues

- [ ] **Step 12: Commit**

```bash
git add theme.go theme_test.go
git commit -m "feat: add theme.conf parser and loader"
```

---

## Task 2: Wire theme vars into ui.go and main.go

**Files:**
- Modify: `ui.go:14-46` (delete the old color scheme const/var block — now in theme.go)
- Modify: `ui.go:110,113,118` (body backgrounds)
- Modify: `ui.go:132-134` (playlist selected style)
- Modify: `ui.go:161-162` (browser selected style)
- Modify: `ui.go:491` (help screen title bar)
- Modify: `main.go:159-162` (call `loadTheme()` before `NewUI()`)

**Interfaces:**
- Consumes from Task 1: `colHeader`, `colPaleText`, `colError`, `borderColorPlaylist`, `borderColorCoverArt`, `borderColorSpectrum`, `colBodyBG`, `colPlaylistSelFG`, `colPlaylistSelBG`, `colBrowserSelFG`, `colBrowserSelBG`, `colBarFill`, `colAmber`, `colTeal`, `colReset`, `colorHex()`, `loadTheme()`.
- Produces: no new symbols — `ui.go` now compiles against `theme.go`'s vars instead of declaring its own. This task has no new test file; `go build`/`go vet`/`go test ./...` in Step 6 is the safety net, since every var defaults to the exact value `ui.go` previously hardcoded, so no existing test should change behavior.

- [ ] **Step 1: Delete the old color declarations from ui.go**

In `ui.go`, delete the entire block from the `// Color scheme:` comment through the closing `)` of the border-color `var` block (originally lines 14-46):

```go
// Color scheme: pale and muted, like btop's default theme - dark neutral
// backgrounds, desaturated accent colors instead of saturated named colors.
//
//	header/status bars:  dark gray bg, pale gray foreground
//	playing / progress:  muted teal
//	directories:         muted teal
//	errors:              muted red
//	secondary/hints:     muted amber
const (
	colBarFill = "[#2a2a2a:#6b9b8f]"
	colReset   = "[-:-]"
	colAmber   = "[#c9b46b]" // replaces bright [yellow] tags
	colTeal    = "[#6b9b9b]" // replaces bright [aqua]/[cyan] tags
)

var (
	colHeader   = tcell.NewRGBColor(0x3a, 0x3a, 0x3a)
	colPaleText = tcell.NewRGBColor(0xc0, 0xc0, 0xc0)
	colError    = tcell.NewRGBColor(0xa8, 0x6b, 0x6b) // muted dusty red
)

const (
	CoverArtWidth  = 32
	CoverArtHeight = 16
)

// Border accent colors, one per panel (btop assigns each box its own
// accent color rather than a single uniform border) - desaturated pastel
// tones rather than tcell's saturated named colors.
var (
	borderColorPlaylist = tcell.NewRGBColor(0x6b, 0x9b, 0x9b) // muted teal
	borderColorCoverArt = tcell.NewRGBColor(0xa8, 0x8b, 0xb5) // muted mauve
	borderColorSpectrum = tcell.NewRGBColor(0x8f, 0xb0, 0x8a) // muted sage
)
```

Replace it with just the part that has no home in theme.go (the layout constants, unrelated to color):

```go
const (
	CoverArtWidth  = 32
	CoverArtHeight = 16
)
```

- [ ] **Step 2: Replace the three hardcoded body backgrounds**

In `ui.go`, change:

```go
	u.progress = tview.NewTextView().SetDynamicColors(true)
	u.progress.SetBackgroundColor(tcell.ColorBlack)

	u.spectrum = tview.NewTextView().SetDynamicColors(true)
	u.spectrum.SetBackgroundColor(tcell.ColorBlack)
	u.spectrum.SetBorder(true).SetTitle(" Spectrum ")
	u.spectrum.SetBorderColor(borderColorSpectrum)

	u.coverArt = tview.NewTextView().SetDynamicColors(true)
	u.coverArt.SetBackgroundColor(tcell.ColorBlack)
	u.coverArt.SetBorder(true).SetTitle(" Cover ")
	u.coverArt.SetBorderColor(borderColorCoverArt)
```

to:

```go
	u.progress = tview.NewTextView().SetDynamicColors(true)
	u.progress.SetBackgroundColor(colBodyBG)

	u.spectrum = tview.NewTextView().SetDynamicColors(true)
	u.spectrum.SetBackgroundColor(colBodyBG)
	u.spectrum.SetBorder(true).SetTitle(" Spectrum ")
	u.spectrum.SetBorderColor(borderColorSpectrum)

	u.coverArt = tview.NewTextView().SetDynamicColors(true)
	u.coverArt.SetBackgroundColor(colBodyBG)
	u.coverArt.SetBorder(true).SetTitle(" Cover ")
	u.coverArt.SetBorderColor(borderColorCoverArt)
```

- [ ] **Step 3: Replace the playlist and browser selected-item styles**

In `ui.go`, change:

```go
	u.playlist.SetSelectedStyle(tcell.StyleDefault.
		Foreground(tcell.ColorWhite).Background(tcell.ColorBlack).
		Bold(true))
```

to:

```go
	u.playlist.SetSelectedStyle(tcell.StyleDefault.
		Foreground(colPlaylistSelFG).Background(colPlaylistSelBG).
		Bold(true))
```

And change:

```go
	u.browserList.SetSelectedStyle(tcell.StyleDefault.
		Foreground(tcell.ColorBlack).Background(tcell.ColorWhite))
```

to:

```go
	u.browserList.SetSelectedStyle(tcell.StyleDefault.
		Foreground(colBrowserSelFG).Background(colBrowserSelBG))
```

- [ ] **Step 4: Fix the duplicated hex literal in the help screen title bar**

In `ui.go`, inside `ShowHelp`, change:

```go
	lines := []string{
		"[#c0c0c0:#3a3a3a] muzak321 - Music Player [-:-]",
```

to:

```go
	lines := []string{
		"[#" + colorHex(colPaleText) + ":" + colorHex(colHeader) + "] muzak321 - Music Player [-:-]",
```

- [ ] **Step 5: Call `loadTheme()` before the UI is built**

In `main.go`, change:

```go
	player := NewPlayer()
	a := &App{player: player, shuffle: *shuffle}
	a.ui = NewUI()
```

to:

```go
	loadTheme()

	player := NewPlayer()
	a := &App{player: player, shuffle: *shuffle}
	a.ui = NewUI()
```

- [ ] **Step 6: Build and run the full test suite**

Run: `go build -o /tmp/muzak321-build . && go vet ./... && go test ./...`
Expected: build succeeds, `go vet` clean, all existing tests still PASS (colors are unchanged at defaults, so no behavioral test should regress).

- [ ] **Step 7: Commit**

```bash
git add ui.go main.go
git commit -m "refactor: wire ui.go colors through the theme.go vars"
```

---

## Task 3: Rewrite spectrumColor as a 3-keyframe gradient

**Files:**
- Modify: `spectrum.go:1-8` (imports)
- Modify: `spectrum.go:193-208` (the `spectrumColor` function)
- Test: `theme_test.go`

**Interfaces:**
- Consumes from Task 1: `spectrumLow`, `spectrumMid`, `spectrumHigh tcell.Color`, `hexToColor()`.
- Produces: `spectrumColor(v float64) string` — same signature, same 6-hex-digit return format as before; callers in `spectrum.go` (`fmt.Fprintf(&sb, "[#%s]%c[-]", spectrumColor(values[c]), ...)`) are unchanged.

- [ ] **Step 1: Write tests pinning exact output at the default palette**

These values are derived algebraically from the *current* formula (see the
design spec) — a piecewise lerp across `spectrum_low=#5fb05f`,
`spectrum_mid=#b0b05f`, `spectrum_high=#b05f5f` reproduces it exactly at
these five points, so this test both documents and regression-proofs the
rewrite in Step 2.

```go
// append to theme_test.go

func TestSpectrumColorDefaultPalette(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0.0, "5fb05f"},
		{0.25, "87b05f"},
		{0.5, "b0b05f"},
		{0.75, "b0875f"},
		{1.0, "b05f5f"},
	}
	for _, c := range cases {
		if got := spectrumColor(c.v); got != c.want {
			t.Errorf("spectrumColor(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestSpectrumColorCustomKeyframes(t *testing.T) {
	origLow, origMid, origHigh := spectrumLow, spectrumMid, spectrumHigh
	t.Cleanup(func() { spectrumLow, spectrumMid, spectrumHigh = origLow, origMid, origHigh })

	spectrumLow, _ = hexToColor("#000000")
	spectrumMid, _ = hexToColor("#808080")
	spectrumHigh, _ = hexToColor("#ffffff")

	if got := spectrumColor(0.0); got != "000000" {
		t.Errorf("spectrumColor(0.0) = %q, want %q", got, "000000")
	}
	if got := spectrumColor(1.0); got != "ffffff" {
		t.Errorf("spectrumColor(1.0) = %q, want %q", got, "ffffff")
	}
}
```

- [ ] **Step 2: Run to verify it currently passes against the OLD formula too**

Run: `go test ./... -run 'TestSpectrumColor' -v`
Expected: `TestSpectrumColorDefaultPalette` PASSES already (the two formulas are
numerically identical at these five points — that's the point of the
design). `TestSpectrumColorCustomKeyframes` FAILS, since the old
`spectrumColor` doesn't read `spectrumLow`/`spectrumMid`/`spectrumHigh` at
all. This failure is what Step 3 fixes.

- [ ] **Step 3: Rewrite spectrumColor**

In `spectrum.go`, update the import block to add `tcell`:

```go
import (
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/gdamore/tcell/v2"
)
```

Then replace:

```go
// spectrumColor returns a muted green→amber→red truecolor hex string for a
// value in [0,1]. Intensity is capped well below full saturation (btop-style
// pale/dusty palette rather than bright primary colors).
func spectrumColor(v float64) string {
	const lo, hi = 0x5f, 0xb0 // dim floor / muted ceiling per channel
	var r, g int
	switch {
	case v < 0.5:
		r = lo + int(float64(hi-lo)*v*2)
		g = hi
	default:
		r = hi
		g = hi - int(float64(hi-lo)*(v-0.5)*2)
	}
	return fmt.Sprintf("%02x%02x%02x", r, g, lo)
}
```

with:

```go
// spectrumColor returns a truecolor hex string for a value in [0,1],
// linearly interpolated across the theme's spectrum_low → spectrum_mid →
// spectrum_high keyframes (spectrum_low at v=0, spectrum_mid at v=0.5,
// spectrum_high at v=1). With the default palette this reproduces the
// original btop-style green→amber→red gradient exactly.
func spectrumColor(v float64) string {
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	var from, to tcell.Color
	var t float64
	if v < 0.5 {
		from, to, t = spectrumLow, spectrumMid, v*2
	} else {
		from, to, t = spectrumMid, spectrumHigh, (v-0.5)*2
	}
	r1, g1, b1 := from.RGB()
	r2, g2, b2 := to.RGB()
	r := int(float64(r1) + float64(r2-r1)*t)
	g := int(float64(g1) + float64(g2-g1)*t)
	b := int(float64(b1) + float64(b2-b1)*t)
	return fmt.Sprintf("%02x%02x%02x", r, g, b)
}
```

- [ ] **Step 4: Run to verify both tests pass**

Run: `go build -o /tmp/muzak321-build . && go test ./... -run 'TestSpectrumColor' -v`
Expected: build succeeds, both `TestSpectrumColorDefaultPalette` and `TestSpectrumColorCustomKeyframes` PASS.

- [ ] **Step 5: Run the full existing spectrum test suite to confirm no regression**

Run: `go test ./... -run 'Spectrum' -v`
Expected: all PASS, including `TestSetSpectrumRender` (it only checks for the presence of `#`/braille dots and row counts, not exact hex values, so it's unaffected by the rewrite).

- [ ] **Step 6: Run the full test suite and vet**

Run: `go build -o /tmp/muzak321-build . && go vet ./... && go test ./...`
Expected: all PASS, no vet issues

- [ ] **Step 7: Commit**

```bash
git add spectrum.go theme_test.go
git commit -m "refactor: rewrite spectrumColor as a themeable 3-keyframe gradient"
```

---

## Task 4: Document theme.conf in the README

**Files:**
- Modify: `README.md` (insert a new section before `## Screen Shots`)

**Interfaces:**
- Consumes: the finished key table from Task 1 (no code dependency — this is documentation only, but must accurately reflect the 17 keys `theme.go` actually implements).

- [ ] **Step 1: Insert a "Custom Themes" section into README.md**

Insert this new section immediately before the existing `## Screen Shots` heading:

```markdown
## Custom Themes

muzak321 reads an optional theme file at
`$XDG_CONFIG_HOME/muzak321/theme.conf` (falling back to
`~/.config/muzak321/theme.conf` if `XDG_CONFIG_HOME` isn't set). If the
file doesn't exist, the built-in pale/muted color scheme is used
unchanged.

Format: one `key = #rrggbb` pair per line. Blank lines and lines starting
with `#` are ignored.

```conf
# ~/.config/muzak321/theme.conf
header_bg = #3a3a3a
header_fg = #c0c0c0
```

Any line with an unknown key or an invalid color prints a warning to
stderr and keeps that key's default — it never stops the app from
starting.

Available keys and their defaults:

| Key | Default | Controls |
| --- | --- | --- |
| `header_bg` | `#3a3a3a` | Header/status bar background |
| `header_fg` | `#c0c0c0` | Header/status bar text |
| `error_bg` | `#a86b6b` | Status bar background on error |
| `bar_fill_bg` | `#2a2a2a` | Progress bar track background |
| `bar_fill_fg` | `#6b9b8f` | Progress bar filled portion |
| `accent_amber` | `#c9b46b` | Hints, track numbers, headings |
| `accent_teal` | `#6b9b9b` | Directory entries in the file browser |
| `border_playlist` | `#6b9b9b` | Playlist panel border |
| `border_coverart` | `#a88bb5` | Cover art panel border |
| `border_spectrum` | `#8fb08a` | Spectrum panel border |
| `spectrum_low` | `#5fb05f` | Spectrum color at 0% level |
| `spectrum_mid` | `#b0b05f` | Spectrum color at 50% level |
| `spectrum_high` | `#b05f5f` | Spectrum color at 100% level |
| `body_bg` | `#000000` | Progress/spectrum/cover art panel backgrounds |
| `playlist_selected_fg` | `#ffffff` | Selected playlist row text |
| `playlist_selected_bg` | `#000000` | Selected playlist row background |
| `browser_selected_fg` | `#000000` | Selected file-browser row text |
| `browser_selected_bg` | `#ffffff` | Selected file-browser row background |
```

- [ ] **Step 2: Proofread the inserted section against theme.go**

Run: `grep -o '"[a-z_]*":' theme.go | sort -u` and confirm every key it lists appears in the README table above (17 keys), with matching default hex values (`grep -n 'mustColor\|BGHex\|FGHex\|Hex =' theme.go`).

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document theme.conf in README"
```
