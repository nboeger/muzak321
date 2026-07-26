package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

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
		goncurses.InitPair(1, goncurses.C_WHITE, goncurses.C_BLACK)
		goncurses.InitPair(2, goncurses.C_BLACK, goncurses.C_MAGENTA)
		goncurses.InitPair(3, goncurses.C_BLACK, goncurses.C_WHITE)
		goncurses.InitPair(4, goncurses.C_YELLOW, goncurses.C_BLACK)
		goncurses.InitPair(5, goncurses.C_GREEN, goncurses.C_BLACK)
		goncurses.InitPair(6, goncurses.C_BLACK, goncurses.C_BLACK)
		goncurses.InitPair(7, goncurses.C_CYAN, goncurses.C_BLACK)
		goncurses.InitPair(8, goncurses.C_RED, goncurses.C_BLACK)
		goncurses.InitPair(9, goncurses.C_BLACK, goncurses.C_GREEN)
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

func (s *Screen) PollResize() bool {
	oldRows, oldCols := s.rows, s.cols
	goncurses.ResizeTerm(0, 0)
	s.updateSize()
	if s.rows != oldRows || s.cols != oldCols {
		s.win.Clear()
		return true
	}
	return false
}

func (s *Screen) Clear() {
	s.win.Clear()
}

func (s *Screen) Refresh() {
	s.win.Refresh()
}

func (s *Screen) GetKey() goncurses.Key {
	for {
		key := s.win.GetChar()
		if key != -1 {
			return key
		}
	}
}

func (s *Screen) clearLine(y int) {
	s.win.MovePrint(y, 0, strings.Repeat(" ", s.cols-1))
}

func (s *Screen) Title(name string, state PlayerState, vol float64) {
	s.win.ColorOn(2)
	s.clearLine(0)

	stateMark := ">"
	stateLabel := "PLAYING"
	switch state {
	case StatePlaying:
		stateMark = ">"
		stateLabel = "PLAYING"
	case StatePaused:
		stateMark = "||"
		stateLabel = "PAUSED"
	case StateMuted:
		stateMark = "!"
		stateLabel = "MUTED"
	case StateStopped:
		stateMark = "[]"
		stateLabel = "STOPPED"
	}

	right := fmt.Sprintf(" %s %s  Vol:%3.0f%%", stateMark, stateLabel, vol*100)
	left := " o " + name

	maxLeft := s.cols - len(right) - 3
	if maxLeft < 5 {
		maxLeft = 5
	}
	if len(left) > maxLeft {
		left = "..." + left[len(left)-maxLeft+3:]
	}

	s.win.MovePrint(0, 1, left)
	s.win.MovePrint(0, s.cols-len(right)-1, right)
	s.win.ColorOn(1)
}

func (s *Screen) Progress(pos, dur time.Duration) {
	s.clearLine(1)

	barWidth := s.cols - 15
	if barWidth < 4 {
		barWidth = 4
	}

	var progress float64
	if dur > 0 {
		progress = float64(pos) / float64(dur)
	}
	if progress > 1 {
		progress = 1
	}

	filled := int(progress * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	timeStr := fmt.Sprintf(" %02d:%02d/%02d:%02d",
		int(pos.Minutes()), int(pos.Seconds())%60,
		int(dur.Minutes()), int(dur.Seconds())%60)

	s.win.ColorOn(9)
	if filled > 0 {
		s.win.MovePrint(1, 1, strings.Repeat(" ", filled))
	}
	s.win.ColorOn(6)
	if barWidth-filled > 0 {
		s.win.MovePrint(1, 1+filled, strings.Repeat(" ", barWidth-filled))
	}
	s.win.ColorOn(4)
	s.win.MovePrint(1, 1+barWidth+1, timeStr)
	s.win.ColorOn(1)
}

func (s *Screen) DeviceInfo(name string, vol ...float64) {
	s.clearLine(1)

	volume := 0.8
	if len(vol) > 0 {
		volume = vol[0]
	}

	barWidth := s.cols - 12
	if barWidth < 4 {
		barWidth = 4
	}

	filled := int(volume / 2.0 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	s.win.ColorOn(9)
	if filled > 0 {
		s.win.MovePrint(1, 1, strings.Repeat(" ", filled))
	}
	s.win.ColorOn(6)
	if barWidth-filled > 0 {
		s.win.MovePrint(1, 1+filled, strings.Repeat(" ", barWidth-filled))
	}
	volStr := fmt.Sprintf(" Vol:%3.0f%%", volume*100)
	s.win.ColorOn(4)
	s.win.MovePrint(1, 1+barWidth+1, volStr)
	s.win.ColorOn(1)
}

func (s *Screen) Playlist(files []string, currentIdx int) {
	s.updateSize()
	listStart := 3
	listEnd := s.rows - 2
	avail := listEnd - listStart
	if avail < 1 {
		return
	}

	for r := listStart; r < listEnd; r++ {
		s.clearLine(r)
	}

	if len(files) == 0 {
		s.win.ColorOn(4)
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

	end := s.playlistScroll + avail
	if end > len(files) {
		end = len(files)
	}

	if s.playlistScroll > 0 {
		s.win.ColorOn(4)
		s.win.MovePrint(listStart, 2, fmt.Sprintf("^ %d more", s.playlistScroll))
		s.win.ColorOn(1)
	}

	for i := s.playlistScroll; i < end; i++ {
		row := listStart + (i - s.playlistScroll)

		if s.playlistScroll > 0 && i == s.playlistScroll {
			continue
		}

		name := cleanFileName(files[i])
		numStr := fmt.Sprintf("%2d", i+1)

		availName := s.cols - 8
		if availName < 5 {
			availName = 5
		}
		if len(name) > availName {
			name = name[:availName-3] + "..."
		}

		if i == currentIdx {
			s.win.ColorOn(9)
			s.win.AttrOn(goncurses.A_BOLD)
			s.clearLine(row)
			s.win.MovePrint(row, 2, "> "+name+" <")
			s.win.AttrOff(goncurses.A_BOLD)
			s.win.ColorOn(1)
			s.win.ColorOn(5)
			s.win.MovePrint(row, s.cols-9, "PLAYING")
			s.win.ColorOn(1)
		} else {
			s.win.ColorOn(4)
			s.win.MovePrint(row, 2, numStr)
			s.win.ColorOn(1)
			s.win.MovePrint(row, 5, name)
		}
	}

	if end < len(files) {
		remaining := len(files) - end
		s.win.ColorOn(4)
		s.win.MovePrint(listEnd-1, 2, fmt.Sprintf("v %d more", remaining))
		s.win.ColorOn(1)
	}
}

func (s *Screen) StatusBar(state PlayerState, fileName string, errMsg ...string) {
	bottom := s.rows - 1

	if len(errMsg) > 0 && errMsg[0] != "" {
		s.win.ColorOn(8)
		s.clearLine(bottom)
		msg := errMsg[0]
		if len(msg) > s.cols-4 {
			msg = msg[:s.cols-4]
		}
		s.win.MovePrint(bottom, 1, msg)
		s.win.ColorOn(1)
		return
	}

	var stateLabel string
	switch state {
	case StatePlaying:
		stateLabel = "PLAYING"
	case StatePaused:
		stateLabel = "PAUSED"
	case StateMuted:
		stateLabel = "MUTED"
	case StateStopped:
		stateLabel = "STOPPED"
	}

	controls := " [Space]Play/Pause [M]Mute [P/N]Prev/Next [Up/Dn]Vol [Q]Quit"

	s.win.ColorOn(2)
	s.clearLine(bottom)
	s.win.MovePrint(bottom, 1, " "+stateLabel)
	s.win.MovePrint(bottom, 1+1+len(stateLabel), controls)
	s.win.ColorOn(1)
}

func (s *Screen) Browser(entries []DirEntry, cursor int, scrollOffset int, dir string) {
	s.win.Clear()
	s.win.ColorOn(1)

	heading := fmt.Sprintf(" File Browser: %s ", dir)
	if len(heading) > s.cols-4 {
		heading = "..." + heading[len(heading)-s.cols+7:]
	}

	seps := (s.cols - len(heading)) / 2
	if seps < 1 {
		seps = 1
	}
	s.win.ColorOn(2)
	s.win.MovePrint(0, 1, strings.Repeat(" ", s.cols-2))
	s.win.ColorOn(1)
	s.win.MovePrint(0, 1, heading)

	helpLine := s.rows - 1
	s.win.ColorOn(2)
	s.clearLine(helpLine)
	s.win.MovePrint(helpLine, 1, " [Up/Down]Move [Enter]Sel [Backspace]Up [H]Help [Q]Quit")
	s.win.ColorOn(1)

	maxDisplay := s.rows - 3
	if maxDisplay < 1 {
		maxDisplay = 1
	}

	for i := 0; i < maxDisplay && i+scrollOffset < len(entries); i++ {
		idx := i + scrollOffset
		entry := entries[idx]
		y := 2 + i

		s.clearLine(y)

		line := ""
		if entry.IsDir {
			line = "  /" + entry.Name
		} else {
			line = "   " + entry.Name
		}

		if idx == cursor {
			s.win.AttrOn(goncurses.A_REVERSE)
			s.win.MovePrint(y, 2, line)
			s.win.AttrOff(goncurses.A_REVERSE)
		} else {
			if entry.IsDir {
				s.win.ColorOn(7)
				s.win.MovePrint(y, 2, line)
				s.win.ColorOn(1)
			} else {
				s.win.ColorOn(1)
				s.win.MovePrint(y, 2, line)
			}
		}
	}
}

func (s *Screen) ShowHelp() {
	s.win.Clear()

	s.win.ColorOn(2)
	s.win.MovePrint(0, 1, strings.Repeat(" ", s.cols-2))
	s.win.ColorOn(1)
	s.win.MovePrint(0, 2, " Help")
	s.win.MovePrint(0, 8, "muzak321 - MP3 Music Player")

	lines := []string{
		"",
		"  Usage:",
		"    muzak321 -f <file>   Play a file, playlist, directory, or glob",
		"    muzak321 -s          Shuffle playback",
		"    muzak321             File browser mode",
		"",
		"  Controls:",
		"    Space    Play / Pause",
		"    M        Mute / Unmute",
		"    P / N    Prev / Next track",
		"    Up/Down  Volume",
		"    H        This help screen",
		"    Q        Quit",
		"",
		"  Press any key to continue...",
	}

	for i, l := range lines {
		if i+1 < s.rows {
			s.win.MovePrint(i+1, 2, l)
		}
	}
	s.win.Refresh()
	s.win.GetChar()
}

func (s *Screen) ShowDevices(devices []string, selected int) int {
	s.win.Clear()
	s.win.ColorOn(1)

	lines := []string{
		"Audio Output Devices",
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
	lines = append(lines, "", "Press number to select, any other key to cancel")

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
