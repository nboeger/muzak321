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

// Color scheme: darker/paler, like btop defaults.
//
//	header/status bars:  dark slate gray bg, pale foreground
//	playing / progress:  cyan
//	directories:         cyan
//	errors:              red
//	secondary/hints:    yellowish
const (
	colHeader  = tcell.ColorDarkSlateGray
	colBarFill = "[black:cyan]"
	colReset   = "[-:-]"
)

const (
	CoverArtWidth  = 28
	CoverArtHeight = 14
)

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
	b.SetTextColor(tcell.ColorWhite) // pale text on dark header (btop style)
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

	u.coverArt = tview.NewTextView().SetDynamicColors(true)
	u.coverArt.SetBackgroundColor(tcell.ColorBlack)

	// right column: cover art (14 rows) + equalizer (6 rows visible,
	// full 12-row Braille bar fits and overflows slightly for visibility).
	rightCol := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(u.coverArt, CoverArtHeight, 0, false).
		AddItem(u.spectrum, 8, 0, false)

	u.playlist = tview.NewList()
	u.playlist.ShowSecondaryText(false)
	u.playlist.SetHighlightFullLine(false)
	u.playlist.SetWrapAround(false)
	u.playlist.SetSelectedStyle(tcell.StyleDefault.
		Foreground(tcell.ColorWhite).Background(tcell.ColorBlack).
		Bold(true))

	u.statusLeft = newBar()
	u.statusRight = newBar()
	status := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(u.statusLeft, 10, 0, false).
		AddItem(u.statusRight, 0, 1, false)

	// Main body: playlist (left) | right column (art + spectrum).
	body := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(u.playlist, 0, 1, false).
		AddItem(rightCol, CoverArtWidth, 0, false)

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
		colReset, "[yellow]", timeStr) + colReset)
}

// SetSpectrum renders the spectrum as horizontal bars extending leftward
// from the right edge. Each of the visible bands gets one row; bar length
// represents magnitude. Uses Braille 2x4 cells filled right-to-left.
const spectrumRows = 12  // bands shown (reduced from 28 for horizontal layout)

// 9 Braille patterns for horizontal fill within a cell (right column first,
// then left column). Each cell is 2 dots wide; we use 4 levels per cell.
var horizBraille = [9]rune{
	'⠀', // 0: empty
	'⠈', // 1: dot 4 (top-right)
	'⠘', // 2: dots 4,5 (right column top two)
	'⠸', // 3: dots 4,5,6 (right column)
	'⠼', // 4: dots 4,5,6,8 (full right column)
	'⠿', // 5: full right + dot 1 (left col top)
	'⠿', // 6: full right + dots 1,2
	'⠿', // 7: full right + dots 1,2,3
	'⠿', // 8: full (all 8 dots)
}

func (u *UI) SetSpectrum(values []float64, active bool) {
	if len(values) == 0 {
		u.spectrum.SetText("")
		return
	}

	// Downsample 28 bands → spectrumRows (12) by taking max in each group.
	bandPerRow := len(values) / spectrumRows
	if bandPerRow < 1 {
		bandPerRow = 1
	}
	rows := spectrumRows
	levelMax := len(horizBraille) - 1

	// Target horizontal cells per row = actual widget width. Each Braille
	// cell = 1 terminal column, so the bar fills the full spectrum column
	// (no trailing gap on the right regardless of terminal size).
	_, _, w, _ := u.spectrum.GetInnerRect()
	horizCells := w
	if horizCells < 4 {
		horizCells = 4
	}

	var sb strings.Builder
	for row := 0; row < rows; row++ {
		// Aggregate bands for this row
		start := row * bandPerRow
		end := start + bandPerRow
		if end > len(values) {
			end = len(values)
		}
		var maxV float64
		for i := start; i < end; i++ {
			if values[i] > maxV {
				maxV = values[i]
			}
		}

		if !active {
			// Dimmed: just dots
			for c := 0; c < horizCells; c++ {
				sb.WriteString("[dim]·[-]")
			}
		} else {
			// Horizontal bar: filled cells from right, partial cell at boundary.
			filledCells := int(maxV * float64(horizCells))
			if filledCells > horizCells {
				filledCells = horizCells
			}
			frac := maxV*float64(horizCells) - float64(filledCells)
			partialIdx := int(frac * float64(levelMax) + 0.5)
			if partialIdx > levelMax {
				partialIdx = levelMax
			}

			// Render left-to-right: empty cells, then partial, then full cells.
			// But bar grows from RIGHT, so we render: [empty][partial][full...]
			// Actually we want: left side empty, right side filled.
			for c := 0; c < horizCells; c++ {
				var cell rune
				posFromRight := horizCells - c
				if posFromRight > filledCells {
					// left of the bar - empty
					cell = ' '
				} else if posFromRight == filledCells {
					// boundary cell - partial
					cell = horizBraille[partialIdx]
				} else {
					// inside bar - full
					cell = horizBraille[levelMax]
				}
				if cell == ' ' {
					sb.WriteString(" ")
				} else {
					fmt.Fprintf(&sb, "[#%s]%c[-]", spectrumColor(maxV), cell)
				}
			}
		}
		if row < rows-1 {
			sb.WriteByte('\n')
		}
	}
	u.spectrum.SetText(sb.String())
}

// SetCoverArt renders the current file's embedded art inside the playlist
// box. The rendered string is cached so it is not re-rendered on every tick.
func (u *UI) SetCoverArt(data []byte, mime string) {
	if bytes.Equal(data, u.lastCover) {
		return
	}
	u.lastCover = data
	if len(data) == 0 {
		u.coverArt.SetText("")
		return
	}
	u.coverArt.SetText(coverArtBlock(data, CoverArtWidth, CoverArtHeight))
}

func (u *UI) SetPlaylist(files []string, current int) {
	u.playlist.Clear()
	if len(files) == 0 {
		u.playlist.AddItem("[yellow](empty playlist)[-]", "", 0, nil)
		return
	}
	for i, f := range files {
		name := cleanFileName(f)
		if i == current {
			u.playlist.AddItem(fmt.Sprintf("[::b]%d. * %s[::-]", i+1, name), "", 0, nil)
		} else {
			u.playlist.AddItem(fmt.Sprintf("[yellow]%2d[-] %s", i+1, name), "", 0, nil)
		}
	}
	if current >= 0 && current < len(files) {
		u.playlist.SetCurrentItem(current)
	}
}

func (u *UI) SetStatus(state PlayerState, errMsg string) {
	if errMsg != "" {
		u.statusLeft.SetBackgroundColor(tcell.ColorRed)
		u.statusRight.SetBackgroundColor(tcell.ColorRed)
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
		return "[aqua]  /" + e.Name + colReset
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
			fmt.Fprintf(&sb, "[yellow]%s[-] %s\n", ts, l.Text)
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
		u.historyList.AddItem("[yellow](no recently played tracks)[-]", "", 0, nil)
		return
	}
	for _, e := range entries {
		u.historyList.AddItem(cleanFileName(e[1]), formatHistoryTime(e[0])+"  "+e[1], 0, nil)
	}
}

// --- help ---

func (u *UI) ShowHelp() {
	lines := []string{
		"[black:magenta] muzak321 - Music Player [-:-]",
		"",
		"  [yellow]Player[-]",
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
		"  [yellow]File Browser[-]",
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
