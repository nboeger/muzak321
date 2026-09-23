package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

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

func init() {
	// Rounded corners everywhere, btop-style. Use the same runes for the
	// focused variant so a focused box doesn't switch to double lines.
	tview.Borders.TopLeft = '╭'
	tview.Borders.TopRight = '╮'
	tview.Borders.BottomLeft = '╰'
	tview.Borders.BottomRight = '╯'
	tview.Borders.TopLeftFocus = '╭'
	tview.Borders.TopRightFocus = '╮'
	tview.Borders.BottomLeftFocus = '╰'
	tview.Borders.BottomRightFocus = '╯'
}

type UI struct {
	app   *tview.Application
	pages *tview.Pages

	headerLeft  *tview.TextView
	headerRight *tview.TextView
	progress    *tview.TextView
	spectrum    *tview.TextView
	playlist    *tview.List
	coverArt    *tview.TextView
	statusLeft  *tview.TextView
	statusRight *tview.TextView

	lastCover []byte

	browserHeader *tview.TextView
	browserList   *tview.List
	browserStatus *tview.TextView

	help *tview.TextView

	browserEntries []DirEntry
	browserCur     int

	lyricsView *tview.TextView

	historyList *tview.List
}

func newBar() *tview.TextView {
	b := tview.NewTextView()
	b.SetBackgroundColor(colHeader)
	b.SetTextColor(colPaleText) // pale gray text on dark header (btop style)
	return b
}

func NewUI() *UI {
	u := &UI{app: tview.NewApplication()}

	// --- player page ---
	u.headerLeft = newBar()
	u.headerRight = newBar()
	u.headerRight.SetTextAlign(tview.AlignRight)
	header := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(u.headerLeft, 0, 1, false).
		AddItem(u.headerRight, 1, 0, false)

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

	// right column: cover art box (content + 2 border rows) + equalizer
	// box, sized to fit all spectrumRows plus its own border.
	rightCol := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.coverArt, CoverArtHeight+2, 0, false).
		AddItem(u.spectrum, spectrumRows+2, 0, false)

	u.playlist = tview.NewList()
	u.playlist.ShowSecondaryText(false)
	u.playlist.SetHighlightFullLine(false)
	u.playlist.SetWrapAround(false)
	u.playlist.SetSelectedStyle(tcell.StyleDefault.
		Foreground(tcell.ColorWhite).Background(tcell.ColorBlack).
		Bold(true))
	u.playlist.SetBorder(true).SetTitle(" Playlist ")
	u.playlist.SetBorderColor(borderColorPlaylist)

	u.statusLeft = newBar()
	u.statusRight = newBar()
	status := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(u.statusLeft, 10, 0, false).
		AddItem(u.statusRight, 0, 1, false)

	// Main body: playlist (left) | right column (art + spectrum).
	body := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(u.playlist, 0, 1, false).
		AddItem(rightCol, CoverArtWidth+2, 0, false)

	playerPage := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(body, 0, 1, false).
		AddItem(status, 1, 0, false)

	// --- browser page ---
	u.browserHeader = newBar()
	u.browserList = tview.NewList()
	u.browserList.SetBorder(true).SetTitle(" Files ")
	u.browserList.ShowSecondaryText(false)
	u.browserList.SetHighlightFullLine(true)
	u.browserList.SetWrapAround(true)
	u.browserList.SetSelectedStyle(tcell.StyleDefault.
		Foreground(tcell.ColorBlack).Background(tcell.ColorWhite))
	u.browserStatus = newBar()

	browserPage := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.browserHeader, 1, 0, false).
		AddItem(u.browserList, 0, 1, false).
		AddItem(u.browserStatus, 1, 0, false)

	// --- help page ---
	u.help = tview.NewTextView()
	u.help.SetDynamicColors(true)
	u.help.SetBorder(true).SetTitle(" Help ")

	// --- lyrics page ---
	u.lyricsView = tview.NewTextView().SetDynamicColors(true)
	u.lyricsView.SetBorder(true).SetTitle(" Lyrics ")

	// --- history page ---
	u.historyList = tview.NewList()
	u.historyList.SetBorder(true).SetTitle(" Recently Played ")
	u.historyList.ShowSecondaryText(true)
	u.historyList.SetHighlightFullLine(true)
	u.historyList.SetWrapAround(true)

	u.pages = tview.NewPages().
		AddPage("player", playerPage, true, false).
		AddPage("browser", browserPage, true, false).
		AddPage("help", u.help, true, false).
		AddPage("lyrics", u.lyricsView, true, false).
		AddPage("history", u.historyList, true, false)

	u.app.SetRoot(u.pages, true)
	return u
}

func (u *UI) Run()            { u.app.Run() }
func (u *UI) Stop()           { u.app.Stop() }
func (u *UI) Queue(fn func()) { u.app.QueueUpdateDraw(fn) }

func (u *UI) ShowPage(name string) {
	u.pages.SwitchToPage(name)
}

// --- player page ---

func stateMarkLabel(s PlayerState) (mark, label string) {
	switch s {
	case StatePlaying:
		return ">", "PLAYING"
	case StatePaused:
		return "||", "PAUSED"
	case StateMuted:
		return "!", "MUTED"
	default:
		return "[]", "STOPPED"
	}
}

func cleanFileName(path string) string {
	return filepath.Base(path)
}

func (u *UI) SetHeader(name string, state PlayerState, vol float64) {
	mark, label := stateMarkLabel(state)
	u.headerLeft.SetText(" o " + name)
	u.headerRight.SetText(fmt.Sprintf(" %s %s  Vol:%3.0f%%", mark, label, vol*100))
}

func (u *UI) SetProgress(pos, dur time.Duration) {
	_, _, w, _ := u.progress.GetInnerRect()
	if w < 24 {
		w = 80
	}
	barWidth := w - 17
	if barWidth < 4 {
		barWidth = 4
	}
	var frac float64
	if dur > 0 {
		frac = float64(pos) / float64(dur)
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(frac * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	var timeStr string
	if dur > 0 {
		timeStr = fmt.Sprintf(" %02d:%02d/%02d:%02d",
			int(pos.Minutes()), int(pos.Seconds())%60,
			int(dur.Minutes()), int(dur.Seconds())%60)
	} else {
		timeStr = fmt.Sprintf(" %02d:%02d LIVE",
			int(pos.Minutes()), int(pos.Seconds())%60)
	}

	u.progress.SetText(fmt.Sprintf(" %s%s%s%s%s%s%s%s",
		colBarFill, strings.Repeat(" ", filled),
		colReset, "[black]", strings.Repeat(" ", barWidth-filled),
		colReset, colAmber, timeStr) + colReset)
}

// SetSpectrum renders the spectrum as a btop-style dot equalizer: one
// column per frequency band, one row-resolution dot per level. Lit dots are
// small and colored by magnitude; unlit positions are blank (no background
// grid), so the meter reads as scattered small dots rather than a solid
// filled bar.
const (
	spectrumBars = SpectrumBands // one column per band, no downsampling
	spectrumRows = 12            // vertical dot resolution per bar

	dotFull = '·' // lit LED - kept as small as possible
)

func (u *UI) SetSpectrum(values []float64, active bool) {
	if len(values) == 0 {
		u.spectrum.SetText("")
		return
	}

	bars := spectrumBars
	if bars > len(values) {
		bars = len(values)
	}

	// Center the bars within the box's actual inner width.
	_, _, w, _ := u.spectrum.GetInnerRect()
	pad := (w - bars) / 2
	if pad < 0 {
		pad = 0
		bars = w
	}

	// Rows lit per column, out of spectrumRows.
	litRows := make([]int, bars)
	for c := 0; c < bars; c++ {
		v := values[c]
		if v < 0 {
			v = 0
		} else if v > 1 {
			v = 1
		}
		litRows[c] = int(v*float64(spectrumRows) + 0.5)
	}

	var sb strings.Builder
	for row := 0; row < spectrumRows; row++ {
		if row > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(strings.Repeat(" ", pad))
		rowFromBottom := spectrumRows - 1 - row
		for c := 0; c < bars; c++ {
			if active && rowFromBottom < litRows[c] {
				fmt.Fprintf(&sb, "[#%s]%c[-]", spectrumColor(values[c]), dotFull)
			} else {
				sb.WriteByte(' ')
			}
		}
	}
	u.spectrum.SetText(sb.String())
}

// SetCoverArt renders the current file's embedded art inside the cover art
// box. When the terminal supports an inline-graphics protocol (Kitty,
// iTerm2, Sixel) the real image is drawn via SetDrawFunc; otherwise it falls
// back to an ANSI half-block ASCII rendering. The rendered string is cached
// so it is not re-rendered on every tick.
func (u *UI) SetCoverArt(data []byte, mime string) {
	if bytes.Equal(data, u.lastCover) {
		return
	}
	u.lastCover = data
	if len(data) == 0 {
		u.coverArt.SetText("")
		u.coverArt.SetDrawFunc(nil)
		return
	}
	if fn := coverArtDrawFunc(data, CoverArtWidth, CoverArtHeight); fn != nil {
		u.coverArt.SetText("")
		u.coverArt.SetDrawFunc(fn)
		return
	}
	u.coverArt.SetDrawFunc(nil)
	u.coverArt.SetText(coverArtBlock(data, CoverArtWidth, CoverArtHeight))
}

func (u *UI) SetPlaylist(files []string, current int) {
	u.playlist.Clear()
	if len(files) == 0 {
		u.playlist.AddItem(colAmber+"(empty playlist)[-]", "", 0, nil)
		return
	}
	for i, f := range files {
		name := cleanFileName(f)
		if i == current {
			u.playlist.AddItem(fmt.Sprintf("[::b]%d. * %s[::-]", i+1, name), "", 0, nil)
		} else {
			u.playlist.AddItem(fmt.Sprintf("%s%2d[-] %s", colAmber, i+1, name), "", 0, nil)
		}
	}
	if current >= 0 && current < len(files) {
		u.playlist.SetCurrentItem(current)
	}
}

func (u *UI) SetStatus(state PlayerState, errMsg string) {
	if errMsg != "" {
		u.statusLeft.SetBackgroundColor(colError)
		u.statusRight.SetBackgroundColor(colError)
		u.statusLeft.SetText(" ERROR ")
		u.statusRight.SetText(errMsg)
		return
	}
	u.statusLeft.SetBackgroundColor(colHeader)
	u.statusRight.SetBackgroundColor(colHeader)
	_, label := stateMarkLabel(state)
	u.statusLeft.SetText(" " + label)
	u.statusRight.SetText(" [Space]Play/Pause [M]Mute [P/N]Prev/Next [Up/Dn]Vol [A]Add [H]Help [Q]Quit")
}

// --- browser page ---

const browserHints = " [Up/Down]Move [Enter]Open/Select [Shift+A]Add all music in dir [Backspace]Up [Esc]Back [H]Help [Q]Quit"

func (u *UI) SetBrowser(dir string, entries []DirEntry, current int) {
	u.browserHeader.SetText(" File Browser: " + dir)
	u.browserList.Clear()
	u.browserEntries = entries
	u.browserCur = -1
	for _, e := range entries {
		u.browserList.AddItem(u.browserItemText(e), "", 0, nil)
	}
	u.SetBrowserCurrent(current)
	u.SetBrowserStatus(browserHints)
}

func (u *UI) browserItemText(e DirEntry) string {
	if e.IsDir {
		return colTeal + "  /" + e.Name + colReset
	}
	return "   " + e.Name
}

func (u *UI) SetBrowserCurrent(idx int) {
	if len(u.browserEntries) == 0 {
		u.browserCur = -1
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(u.browserEntries) {
		idx = len(u.browserEntries) - 1
	}
	u.browserList.SetCurrentItem(idx)

	// Restore the color tag on the previously selected item, strip it from the
	// newly selected one so the reverse (black on white) highlight stays clean.
	if u.browserCur >= 0 && u.browserCur != idx && u.browserCur < len(u.browserEntries) {
		u.browserList.SetItemText(u.browserCur, u.browserItemText(u.browserEntries[u.browserCur]), "")
	}
	if u.browserEntries[idx].IsDir {
		u.browserList.SetItemText(idx, "  /"+u.browserEntries[idx].Name, "")
	}
	u.browserCur = idx
}

func (u *UI) SetBrowserStatus(msg string) {
	u.browserStatus.SetText(msg)
}

// SetLyrics renders the synced lyrics with the current line highlighted and
// scrolled into view.
func (u *UI) SetLyrics(lines []LyricLine, current int) {
	var sb strings.Builder
	for i, l := range lines {
		ts := fmt.Sprintf("%02d:%02d", int(l.At.Minutes()), int(l.At.Seconds())%60)
		if i == current {
			fmt.Fprintf(&sb, "[lime]%s [::b]%s[::-]\n", ts, l.Text)
		} else {
			fmt.Fprintf(&sb, "%s%s[-] %s\n", colAmber, ts, l.Text)
		}
	}
	u.lyricsView.SetText(sb.String())
	if current >= 0 {
		u.lyricsView.ScrollTo(0, current)
	}
}

// SetHistory fills the recently-played list (most recent first).
func (u *UI) SetHistory(entries [][]string) {
	u.historyList.Clear()
	if len(entries) == 0 {
		u.historyList.AddItem(colAmber+"(no recently played tracks)[-]", "", 0, nil)
		return
	}
	for _, e := range entries {
		u.historyList.AddItem(cleanFileName(e[1]), formatHistoryTime(e[0])+"  "+e[1], 0, nil)
	}
}

// --- help ---

func (u *UI) ShowHelp() {
	lines := []string{
		"[#c0c0c0:#3a3a3a] muzak321 - Music Player [-:-]",
		"",
		"  " + colAmber + "Player[-]",
		"    Space          Play / Pause",
		"    M              Mute / Unmute",
		"    P / N          Prev / Next track",
		"    Up/Down        Volume",
		"    Left/Right     Seek -5s / +5s (Shift: -30s / +30s)",
		"    Home/End       Seek start / end of track",
		"    L              Toggle synced lyrics (.lrc)",
		"    S              Save queue (last.m3u) / Shift+S reload + play",
		"    Y              Recently played history",
		"    A              Open file browser (add songs)",
		"    H              This help",
		"    Q              Quit",
		"",
		"  " + colAmber + "File Browser[-]",
		"    Up/Down        Move",
		"    Enter          Open directory / add selected file",
		"    Shift+A        Add every music file in the current directory",
		"    Backspace      Go up one directory",
		"    Esc            Back to the player",
		"    H              This help",
		"    Q              Quit",
		"",
		"  [lime]Press any key to return...[-]",
	}
	u.help.SetText(strings.Join(lines, "\n"))
}
