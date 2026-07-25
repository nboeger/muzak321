package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rthornton128/goncurses"
)

type Screen struct {
	win            *goncurses.Window
	rows           int
	cols           int
	playlistScroll int
}

func NewScreen() (*Screen, error) {
	win, err := goncurses.Init()
	if err != nil {
		return nil, fmt.Errorf("ncurses init: %w", err)
	}

	goncurses.Raw(true)
	goncurses.Echo(false)
	goncurses.Cursor(0)
	win.Keypad(true)
	win.Timeout(50)

	if goncurses.HasColors() {
		goncurses.StartColor()
		goncurses.InitPair(1, goncurses.C_WHITE, goncurses.C_BLUE)
		goncurses.InitPair(2, goncurses.C_CYAN, goncurses.C_BLUE)
		goncurses.InitPair(3, goncurses.C_WHITE, goncurses.C_BLUE)
		goncurses.InitPair(10, goncurses.C_YELLOW, goncurses.C_BLUE)
		goncurses.InitPair(11, goncurses.C_GREEN, goncurses.C_BLUE)
		goncurses.InitPair(12, goncurses.C_CYAN, goncurses.C_BLUE)
		goncurses.InitPair(13, goncurses.C_RED, goncurses.C_BLUE)
		goncurses.InitPair(14, goncurses.C_MAGENTA, goncurses.C_BLUE)
	}

	win.SetBackground(goncurses.ColorPair(1))
	win.Clear()
	win.ColorOn(1)

	s := &Screen{win: win}
	s.updateSize()
	return s, nil
}

func (s *Screen) updateSize() {
	s.rows, s.cols = s.win.MaxYX()
}

func (s *Screen) Close() {
	goncurses.End()
}

func (s *Screen) NeedResize() bool {
	return goncurses.IsTermResized(s.rows, s.cols)
}

func (s *Screen) Resize() {
	goncurses.ResizeTerm(0, 0)
	s.updateSize()
	s.win.Clear()
}

func (s *Screen) Clear() {
	s.win.Clear()
}

func (s *Screen) Refresh() {
	s.win.Refresh()
}

func (s *Screen) GetKey() goncurses.Key {
	// Loop because getch() can return ERR (-1) when interrupted
	// by Go runtime signals (SIGURG, etc.)
	for {
		key := s.win.GetChar()
		if key != -1 {
			return key
		}
	}
}

func (s *Screen) Title(name string) {
	s.win.ColorOn(1)
	s.clearLine(0)
	display := name
	if len(display) > s.cols-2 {
		display = "..." + display[len(display)-s.cols+5:]
	}
	s.win.MovePrint(0, (s.cols-len(display))/2, display)
}

func (s *Screen) DeviceInfo(name string, vol ...float64) {
	s.win.ColorOn(12)
	s.clearLine(1)
	disp := "Device: " + name
	if len(vol) > 0 {
		disp += fmt.Sprintf("  Vol:%.0f%%", vol[0]*100)
	}
	if len(disp) > s.cols-4 {
		disp = disp[:s.cols-4]
	}
	s.win.MovePrint(1, s.cols-len(disp)-1, disp)
	s.win.ColorOn(1)
}

func (s *Screen) clearLine(y int) {
	s.win.MovePrint(y, 0, strings.Repeat(" ", s.cols-1))
}

func (s *Screen) Playlist(files []string, currentIdx int) {
	s.updateSize()
	listStart := 3
	listEnd := s.rows - 2
	avail := listEnd - listStart
	if avail < 1 {
		return
	}

	if len(files) == 0 {
		for r := listStart; r < listEnd; r++ {
			s.clearLine(r)
		}
		s.win.ColorOn(12)
		s.win.MovePrint(listStart, 2, "(empty playlist)")
		s.win.ColorOn(1)
		return
	}

	if currentIdx < s.playlistScroll {
		s.playlistScroll = currentIdx
	}
	if currentIdx >= s.playlistScroll+avail {
		s.playlistScroll = currentIdx - avail + 1
	}

	for r := listStart; r < listEnd; r++ {
		s.clearLine(r)
	}

	end := s.playlistScroll + avail
	if end > len(files) {
		end = len(files)
	}
	for i := s.playlistScroll; i < end; i++ {
		row := listStart + (i - s.playlistScroll)
		name := cleanFileName(files[i])
		if len(name) > s.cols-6 {
			name = "..." + name[len(name)-s.cols+9:]
		}
		if i == currentIdx {
			s.win.ColorOn(14)
			s.win.MovePrint(row, 2, "> "+name)
		} else {
			s.win.ColorOn(1)
			s.win.MovePrint(row, 2, "  "+name)
		}
	}
	s.win.ColorOn(1)
}

func (s *Screen) StatusBar(state PlayerState, fileName string, errMsg ...string) {
	bottom := s.rows - 1
	s.clearLine(bottom)
	s.win.ColorOn(1)

	var status string
	switch state {
	case StatePlaying:
		status = "PLAYING"
	case StatePaused:
		status = "PAUSED"
	case StateMuted:
		status = "MUTED"
	case StateStopped:
		status = "STOPPED"
	}

	if len(errMsg) > 0 && errMsg[0] != "" {
		msg := errMsg[0]
		if len(msg) > s.cols-4 {
			msg = msg[:s.cols-4]
		}
		s.win.MovePrint(bottom, 1, msg)
		s.win.ColorOn(13)
		restart := fmt.Sprintf("  Press [Space] '%s' or [N] Next", fileName)
		if 1+len(msg)+len(restart) < s.cols-1 {
			s.win.MovePrint(bottom, 1+len(msg), restart)
		}
		return
	}

	statusStr := fmt.Sprintf(" [%s]  ", status)
	controls := "[Space] Play/Pause  [M] Mute  [P/N] Prev/Next  [Up/Dn] Vol  [Q] Quit"


	s.win.ColorOn(1)
	s.win.MovePrint(bottom, 1, statusStr)
	s.win.ColorOn(11)

	controlsX := len(statusStr) + 2
	if controlsX < s.cols-2 {
		s.win.MovePrint(bottom, controlsX, controls)
	}
	s.win.ColorOn(1)
}

func (s *Screen) Browser(entries []DirEntry, cursor int, scrollOffset int, dir string) {
	s.win.Clear()
	s.win.ColorOn(1)

	heading := fmt.Sprintf(" Select Files — %s", dir)
	if len(heading) > s.cols-2 {
		heading = "..." + heading[len(heading)-s.cols+5:]
	}
	s.win.MovePrint(0, (s.cols-len(heading))/2, heading)

	helpLine := s.rows - 1
	s.clearLine(helpLine)
	s.win.MovePrint(helpLine, 1, "[Up/Down] Navigate  [Enter] Select  [Backspace] Up  [Q] Quit")

	maxDisplay := s.rows - 3
	if maxDisplay < 1 {
		maxDisplay = 1
	}

	for i := 0; i < maxDisplay && i+scrollOffset < len(entries); i++ {
		idx := i + scrollOffset
		entry := entries[idx]
		y := 2 + i

		line := ""
		if entry.IsDir {
			line = "/" + entry.Name
		} else {
			line = " " + entry.Name
		}

		if idx == cursor {
			s.win.ColorOn(11)
			s.win.AttrOn(goncurses.A_REVERSE)
			s.clearLine(y)
			s.win.MovePrint(y, 2, line)
			s.win.AttrOff(goncurses.A_REVERSE)
			s.win.ColorOn(1)
		} else {
			s.clearLine(y)
			if entry.IsDir {
				s.win.ColorOn(12)
			} else {
				s.win.ColorOn(1)
			}
			s.win.MovePrint(y, 2, line)
			s.win.ColorOn(1)
		}
	}
}

func (s *Screen) ShowHelp() {
	s.win.Clear()
	s.win.ColorOn(1)

	lines := []string{
		" muzak321 — MP3 Music Player",
		"",
		" Usage:",
		"   muzak321 -f <playlist.m3u>   Play an M3U playlist",
		"   muzak321 -s                   Shuffle playback",
		"   muzak321 -h                   Show this help",
		"   muzak321                      File browser mode",
		"",
		" Controls:",
		"   Space    Play / Pause",
		"   M        Mute / Unmute",
		"   P/N      Prev / Next track",
		"   Up/Down  Volume",
		"   Q        Quit",
		"",
		" Press any key to continue...",
	}

	for i, l := range lines {
		if i < s.rows {
			s.win.MovePrint(i, 2, l)
		}
	}
	s.win.Refresh()
	s.win.GetChar()
}

func (s *Screen) ShowDevices(devices []string, selected int) int {
	s.win.Clear()
	s.win.ColorOn(1)

	lines := []string{
		" Audio Output Devices",
		"",
	}
	for i, d := range devices {
		mark := "  "
		if i == selected {
			mark = " >"
		}
		line := fmt.Sprintf("%s[%d] %s", mark, i, d)
		lines = append(lines, line)
	}
	lines = append(lines, "", " Press number to select, any other key to cancel")

	for i, l := range lines {
		if i < s.rows {
			s.win.MovePrint(i, 2, l)
		}
	}
	s.win.Refresh()

	for {
		key := s.win.GetChar()
		if key >= '0' && key <= '9' {
			n := int(key - '0')
			if n >= 0 && n < len(devices) {
				return n
			}
		}
		if key >= 0 && key <= 127 {
			return selected
		}
	}
}

func (s *Screen) Message(msg string) {
	s.updateSize()
	mid := s.rows / 2
	s.win.ColorOn(1)
	s.clearLine(mid)
	if len(msg) > s.cols-4 {
		msg = msg[:s.cols-4]
	}
	s.win.MovePrint(mid, (s.cols-len(msg))/2, msg)
}

func cleanFileName(path string) string {
	return filepath.Base(path)
}
