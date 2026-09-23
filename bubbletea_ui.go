package main

import (
	"bytes"
	"fmt"
	"image"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// BubbleTeaUI implements the UI interface using Bubble Tea.
type BubbleTeaUI struct {
	model *teaModel
	app   *tea.Program
}

type teaModel struct {
	// State
	headerLeft, headerRight string
	progress               string
	playlist               []string
	playlistIdx            int
	spectrum               [][]string  // pre-rendered spectrum rows
	coverArt               image.Image
	coverArtKitty          string      // Kitty graphics payload
	status                 string
	page                   string      // "player", "browser", "lyrics", "help"

	// UI dimensions
	width, height int
}

type queuedCommand struct {
	fn func()
}

// Init returns initial commands (none for startup).
func (m *teaModel) Init() tea.Cmd {
	return nil
}

// Update handles messages (keys, app state updates).
func (m *teaModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg), nil
	case queuedCommand:
		msg.fn()
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

// View renders the current state.
func (m *teaModel) View() string {
	switch m.page {
	case "player":
		return m.viewPlayer()
	case "browser":
		return m.viewBrowser()
	case "lyrics":
		return m.viewLyrics()
	case "help":
		return m.viewHelp()
	default:
		return m.viewPlayer()
	}
}

func (m *teaModel) viewPlayer() string {
	return "[Player view]"
}

func (m *teaModel) viewBrowser() string {
	return "[Browser view]"
}

func (m *teaModel) viewLyrics() string {
	return "[Lyrics view]"
}

func (m *teaModel) viewHelp() string {
	return "[Help view]"
}

func (m *teaModel) handleKey(msg tea.KeyMsg) tea.Model {
	switch msg.String() {
	case "q", "ctrl+c":
		return m
	}
	return m
}

// NewBubbleTeaUI creates a new Bubble Tea UI.
func NewBubbleTeaUI() *BubbleTeaUI {
	m := &teaModel{
		width:        80,
		height:       24,
		playlist:     []string{},
		playlistIdx:  -1,
		page:         "player",
	}
	p := tea.NewProgram(m)
	return &BubbleTeaUI{
		model: m,
		app:   p,
	}
}

func (u *BubbleTeaUI) Run() {
	if _, err := u.app.Run(); err != nil {
		panic(err)
	}
}

func (u *BubbleTeaUI) Stop() {
	u.app.Quit()
}

func (u *BubbleTeaUI) Queue(fn func()) {
	u.app.Send(queuedCommand{fn: fn})
}

// UI interface implementations (stubs for now)

func (u *BubbleTeaUI) SetHeader(name string, state PlayerState, vol float64) {
	u.Queue(func() {
		u.model.headerLeft = fmt.Sprintf("%s %.0f%%", name, vol*100)
		u.model.headerRight = formatPlayerState(state)
	})
}

func (u *BubbleTeaUI) SetProgress(pos, dur time.Duration) {
	progress := formatProgress(pos, dur)
	u.Queue(func() {
		u.model.progress = progress
	})
}

func (u *BubbleTeaUI) SetSpectrum(values []float64, active bool) {
	if values == nil {
		u.Queue(func() { u.model.spectrum = nil })
		return
	}
	rows := renderSpectrum(values, active)
	u.Queue(func() {
		u.model.spectrum = rows
	})
}

func (u *BubbleTeaUI) SetCoverArt(data []byte, mime string) {
	if len(data) == 0 {
		u.Queue(func() {
			u.model.coverArt = nil
			u.model.coverArtKitty = ""
		})
		return
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return
	}

	kittyPayload := renderKittyImage(img, CoverArtWidth, CoverArtHeight)
	u.Queue(func() {
		u.model.coverArt = img
		u.model.coverArtKitty = kittyPayload
	})
}

func (u *BubbleTeaUI) SetPlaylist(files []string, current int) {
	u.Queue(func() {
		u.model.playlist = files
		u.model.playlistIdx = current
	})
}

func (u *BubbleTeaUI) SetStatus(state PlayerState, errMsg string) {
	status := formatStatus(state, errMsg)
	u.Queue(func() {
		u.model.status = status
	})
}

func (u *BubbleTeaUI) ShowPage(name string) {
	u.Queue(func() {
		u.model.page = name
	})
}

// Browser, lyrics, history, help (stubs)
func (u *BubbleTeaUI) SetBrowser(dir string, entries []DirEntry, current int) {}
func (u *BubbleTeaUI) SetBrowserCurrent(idx int)                               {}
func (u *BubbleTeaUI) SetBrowserStatus(msg string)                             {}
func (u *BubbleTeaUI) SetLyrics(lines []LyricLine, current int)                {}
func (u *BubbleTeaUI) SetHistory(entries [][]string)                           {}
func (u *BubbleTeaUI) ShowHelp()                                               {}

// Helper formatters
func formatPlayerState(state PlayerState) string {
	switch state {
	case StatePlaying:
		return "▶"
	case StatePaused:
		return "⏸"
	case StateStopped:
		return "⏹"
	default:
		return "-"
	}
}

func formatStatus(state PlayerState, errMsg string) string {
	if errMsg != "" {
		return errMsg
	}
	return formatPlayerState(state)
}

func formatProgress(pos, dur time.Duration) string {
	posStr := fmt.Sprintf("%d:%02d", int(pos.Minutes()), int(pos.Seconds())%60)
	durStr := fmt.Sprintf("%d:%02d", int(dur.Minutes()), int(dur.Seconds())%60)
	return fmt.Sprintf("%s / %s", posStr, durStr)
}

func renderSpectrum(values []float64, active bool) [][]string {
	if len(values) == 0 {
		return nil
	}
	// Pre-rendered placeholder - will be implemented in Task 4
	return [][]string{}
}

func renderKittyImage(img image.Image, width, height int) string {
	// Stub - will be implemented in Task 3
	return ""
}
