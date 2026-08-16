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

// Color scheme (kept from the ncurses version):
//
//	header/status bars:  black text on magenta (ncurses pair 2)
//	cursor/reverse:      black on white        (pair 3)
//	secondary/hints:     yellow                (pair 4)
//	playing / progress:  green                 (pair 5 / 9)
//	directories:         cyan                  (pair 7)
//	errors:              red                   (pair 8)
const (
	colHeader  = tcell.ColorFuchsia
	colBarFill = "[black:lime]"
	colReset   = "[-:-]"
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
}

func newBar() *tview.TextView {
	b := tview.NewTextView()
	b.SetBackgroundColor(colHeader)
	b.SetTextColor(tcell.ColorBlack)
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
		AddItem(u.headerRight, 22, 0, false)

	u.progress = tview.NewTextView().SetDynamicColors(true)
	u.progress.SetBackgroundColor(tcell.ColorBlack)

	u.spectrum = tview.NewTextView().SetDynamicColors(true)
	u.spectrum.SetBackgroundColor(tcell.ColorBlack)

	u.playlist = tview.NewList()
	u.playlist.SetBorder(true).SetTitle(" Playlist ")
	u.playlist.ShowSecondaryText(false)
	u.playlist.SetHighlightFullLine(true)
	u.playlist.SetWrapAround(false)
	u.playlist.SetSelectedStyle(tcell.StyleDefault.
		Foreground(tcell.ColorBlack).Background(tcell.ColorLime))

	u.statusLeft = newBar()
	u.statusRight = newBar()
	status := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(u.statusLeft, 10, 0, false).
		AddItem(u.statusRight, 0, 1, false)

	playerPage := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 1, 0, false).
		AddItem(u.progress, 1, 0, false).
		AddItem(u.spectrum, 3, 0, false).
		AddItem(u.playlist, 0, 1, false).
		AddItem(status, 1, 0, false)

	u.coverArt = tview.NewTextView().SetDynamicColors(true)
	u.coverArt.SetBackgroundColor(tcell.ColorBlack)
	playerPage = tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(playerPage, 0, 1, false).
		AddItem(u.coverArt, CoverArtWidth, 0, false)

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

	u.pages = tview.NewPages().
		AddPage("player", playerPage, true, false).
		AddPage("browser", browserPage, true, false).
		AddPage("help", u.help, true, false)

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

const spectrumGlyphs = "▁▂▃▄▅▆▇█"

var spectrumGlyphRunes = []rune(spectrumGlyphs)

// SetSpectrum renders the spectrum frame as 3 rows of block glyphs, one
// column per band (24 levels total per band). active=false renders a dimmed
// flat baseline; empty values (stopped) clear the view.
func (u *UI) SetSpectrum(values []float64, active bool) {
	if len(values) == 0 {
		u.spectrum.SetText("")
		return
	}
	var sb strings.Builder
	for row := 0; row < 3; row++ {
		for _, v := range values {
			if !active {
				// Dimmed flat baseline: one dim block per column, bottom row.
				if row == 2 {
					sb.WriteString("[#444444]▁[-]")
				} else {
					sb.WriteString(" ")
				}
				continue
			}
			level := int(v * 24)
			if level > 24 {
				level = 24
			}
			cells := level - (2-row)*8 // cells filled in this row, bottom-up
			if cells < 0 {
				cells = 0
			}
			if cells > 8 {
				cells = 8
			}
			glyph := " "
			if cells > 0 {
				glyph = string(spectrumGlyphRunes[cells-1])
			}
			fmt.Fprintf(&sb, "[#%s]%s[-]", spectrumColor(v), glyph)
		}
		if row < 2 {
			sb.WriteByte('\n')
		}
	}
	u.spectrum.SetText(sb.String())
}

// SetCoverArt renders the current file's embedded art; empty input clears the
// pane. The rendered string is cached so it is not re-rendered on every tick.
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
			u.playlist.AddItem(fmt.Sprintf("%s > %s <%s", colBarFill, name, colReset), "", 0, nil)
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
