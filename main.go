package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
)

// version is overridden at build time via -ldflags "-X main.version=<ver>".
var version = "dev"

type App struct {
	ui      *UI
	player  *Player
	browser *Browser
	shuffle bool

	page       string
	prevPage   string
	fromPlayer bool
}

func resolvePath(path string) ([]string, error) {
	if isStreamURL(path) {
		return []string{path}, nil
	}
	if strings.ContainsAny(path, "*?[") {
		matches, err := filepath.Glob(path)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no files match: %s", path)
		}
		return expandFiles(matches)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		var matches []string
		err := filepath.Walk(path, func(p string, i os.FileInfo, err error) error {
			if err != nil || i.IsDir() {
				return err
			}
			ext := strings.ToLower(filepath.Ext(p))
			if isAudioFile(ext) || ext == ".m3u" || ext == ".pls" {
				matches = append(matches, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no audio files found in: %s", path)
		}
		return expandFiles(matches)
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".m3u" {
		return parseM3U(path)
	}
	if ext == ".pls" {
		return parsePLS(path)
	}
	return []string{path}, nil
}

func expandFiles(paths []string) ([]string, error) {
	var result []string
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".m3u" {
			files, err := parseM3U(p)
			if err != nil {
				return nil, err
			}
			result = append(result, files...)
		} else if ext == ".pls" {
			files, err := parsePLS(p)
			if err != nil {
				return nil, err
			}
			result = append(result, files...)
		} else if isAudioFile(ext) {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no audio files found")
	}
	return result, nil
}

func main() {
	fileArg := flag.String("f", "", "audio file, playlist, directory, or glob pattern")
	help := flag.Bool("h", false, "Show help")
	versionFlag := flag.Bool("v", false, "Show version")
	flag.BoolVar(versionFlag, "version", false, "Show version")
	shuffle := flag.Bool("s", false, "Shuffle playback")
	flag.Parse()

	if *help {
		printHelp()
		return
	}

	if *versionFlag {
		fmt.Printf("muzak321 %s\n", version)
		return
	}

	player := NewPlayer()
	a := &App{player: player, shuffle: *shuffle}
	a.ui = NewUI()
	a.ui.app.SetInputCapture(a.handleKey)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		a.quit()
	}()

	go a.pumpPlayer()
	go a.pumpProgress()
	go a.pumpSpectrum()

	// Play mode: an explicit -f flag OR any positional argument (file,
	// playlist, directory, glob, or stream URL). Without either, fall
	// back to the file browser.
	sources := playArgs(*fileArg, flag.Args())
	if len(sources) > 0 {
		var allFiles []string
		for _, arg := range sources {
			files, err := resolvePath(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			allFiles = append(allFiles, files...)
		}
		if len(allFiles) == 0 {
			fmt.Fprintln(os.Stderr, "No files to play")
			os.Exit(1)
		}
		player.SetFiles(allFiles, *shuffle)
		a.showPlayer()
		player.Start()
		player.PlayCurrent()
	} else {
		a.browser = NewBrowser()
		if err := a.browser.Navigate("."); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading directory: %v\n", err)
			os.Exit(1)
		}
		a.showBrowser()
	}

	a.ui.Run()
}

// playArgs combines the -f flag value (if any) with the positional
// arguments, so both "muzak321 -f song.mp3" and "muzak321 song.mp3" play.
func playArgs(fileArg string, args []string) []string {
	all := make([]string, 0, len(args)+1)
	if fileArg != "" {
		all = append(all, fileArg)
	}
	return append(all, args...)
}

func printHelp() {
	fmt.Printf("muzak321 %s — Music Player\n", version)
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  muzak321 <file.m3u|file.pls|file.mp3|file.flac|file.ogg|file.wav|directory|stream-url> [more files...]")
	fmt.Println("                    Play a playlist, audio file, directory, or live MP3 stream")
	fmt.Println("  muzak321 -f <file|playlist|directory|glob>")
	fmt.Println("                    Same as above, with an explicit flag")
	fmt.Println("  muzak321 -s                        Shuffle playback")
	fmt.Println("  muzak321 -h                        Show this help")
	fmt.Println("  muzak321 -v                        Show version")
	fmt.Println("  muzak321                           File browser mode")
	fmt.Println()
	fmt.Println("Controls:")
	fmt.Println("  Space    Play / Pause")
	fmt.Println("  M        Mute / Unmute")
	fmt.Println("  P/N      Prev / Next track")
	fmt.Println("  Up/Down  Volume")
	fmt.Println("  <-/->    Seek -5s / +5s (Shift: -30s / +30s)")
	fmt.Println("  Home/End Seek start / end of track")
	fmt.Println("  A        Add songs (opens file browser)")
	fmt.Println("  Q        Quit")
	fmt.Println()
	fmt.Println("In the file browser:")
	fmt.Println("  Enter          Open directory / play selected file or playlist")
	fmt.Println("  Shift+A        Add every playable file in the current directory")
	fmt.Println("  Backspace      Up one directory")
	fmt.Println()
	fmt.Println("Streams (.pls, .m3u, direct MP3 URLs):")
	fmt.Println("  Live SHOUTcast/Icecast streams show the current track via")
	fmt.Println("  ICY StreamTitle metadata; Prev is disabled while live.")
}

// --- player events ---

func (a *App) pumpPlayer() {
	for {
		select {
		case <-a.player.fileChanged:
			a.ui.Queue(a.renderPlayer)
		case state := <-a.player.stateChan:
			a.ui.Queue(func() {
				a.renderPlayer()
				a.ui.SetStatus(state, a.player.Error())
			})
		case <-a.player.metaChan:
			a.ui.Queue(a.renderPlayer)
		}
	}
}

func (a *App) pumpProgress() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		a.ui.Queue(func() {
			if a.page != "player" {
				return
			}
			pos, dur := a.player.Progress()
			a.ui.SetProgress(pos, dur)
		})
	}
}

// pumpSpectrum animates the spectrum equalizer at ~30 fps while the player
// page is visible. When paused the bars freeze dimmed; when stopped the view
// clears.
func (a *App) pumpSpectrum() {
	ticker := time.NewTicker(33 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		a.ui.Queue(func() {
			if a.page != "player" {
				return
			}
			active := a.player.State() == StatePlaying
			a.ui.SetSpectrum(a.player.Spectrum(), active)
		})
	}
}

// --- navigation ---

func (a *App) showPlayer() {
	a.page = "player"
	a.ui.ShowPage("player")
	a.renderPlayer()
}

func (a *App) renderPlayer() {
	p := a.player
	pos, dur := p.Progress()
	name := p.CurrentFile()
	if p.IsStreaming() {
		name = a.streamName(p.CurrentFile())
	} else {
		name = trackDisplayName(name)
	}
	a.ui.SetHeader(name, p.State(), p.Volume())
	a.ui.SetProgress(pos, dur)
	a.ui.SetPlaylist(p.Files(), p.CurrentIndex())
	a.ui.SetCoverArt(p.CoverArt())
	a.ui.SetStatus(p.State(), "")
}

// streamName prefers the live ICY StreamTitle, falling back to a friendly URL name.
func (a *App) streamName(url string) string {
	if t := a.player.StreamTitle(); t != "" {
		return t
	}
	return streamNameFromURL(url)
}

func (a *App) showBrowser() {
	a.page = "browser"
	a.ui.ShowPage("browser")
	a.refreshBrowser()
}

func (a *App) refreshBrowser() {
	entries := a.browser.Entries()
	a.ui.SetBrowser(a.browser.Dir(), entries, 0)
}

// --- input ---

func (a *App) handleKey(ev *tcell.EventKey) *tcell.EventKey {
	switch a.page {
	case "player":
		return a.playerKey(ev)
	case "browser":
		return a.browserKey(ev)
	case "help":
		if ev.Key() == tcell.KeyCtrlC {
			a.quit()
			return nil
		}
		a.page = a.prevPage
		a.ui.ShowPage(a.prevPage)
		return nil
	default:
		return ev
	}
}

func (a *App) playerKey(ev *tcell.EventKey) *tcell.EventKey {
	switch {
	case ev.Key() == tcell.KeyCtrlC || ev.Rune() == 'q' || ev.Rune() == 'Q':
		a.quit()
	case ev.Key() == tcell.KeyUp:
		a.player.SetVolume(a.player.Volume() + 0.1)
		a.renderPlayer()
	case ev.Key() == tcell.KeyDown:
		a.player.SetVolume(a.player.Volume() - 0.1)
		a.renderPlayer()
	case ev.Rune() == ' ':
		if a.player.State() == StatePlaying {
			a.player.Pause()
		} else {
			a.player.Play()
		}
		a.renderPlayer()
	case ev.Rune() == 'm' || ev.Rune() == 'M':
		if a.player.State() == StateMuted {
			a.player.Unmute()
		} else {
			a.player.Mute()
		}
		a.renderPlayer()
	case ev.Rune() == 'n' || ev.Rune() == 'N':
		a.player.Next()
	case ev.Rune() == 'p' || ev.Rune() == 'P':
		a.player.Previous()
	case ev.Key() == tcell.KeyLeft || ev.Key() == tcell.KeyRight:
		delta := SeekStep
		if ev.Modifiers()&tcell.ModShift != 0 {
			delta = SeekStepLarge
		}
		if ev.Key() == tcell.KeyLeft {
			delta = -delta
		}
		if err := a.player.SeekRelative(delta); err != nil {
			a.ui.SetStatus(StateStopped, err.Error())
		} else {
			a.renderPlayer()
		}
	case ev.Key() == tcell.KeyHome:
		if err := a.player.SeekTo(0); err != nil {
			a.ui.SetStatus(StateStopped, err.Error())
		} else {
			a.renderPlayer()
		}
	case ev.Key() == tcell.KeyEnd:
		_, dur := a.player.Progress()
		if err := a.player.SeekTo(dur); err != nil {
			a.ui.SetStatus(StateStopped, err.Error())
		} else {
			a.renderPlayer()
		}
	case ev.Rune() == 'a' || ev.Rune() == 'A':
		a.openBrowser()
	case ev.Rune() == 'h' || ev.Rune() == 'H':
		a.showHelp("player")
	default:
		return ev
	}
	return nil
}

func (a *App) browserKey(ev *tcell.EventKey) *tcell.EventKey {
	switch {
	case ev.Key() == tcell.KeyCtrlC || ev.Rune() == 'q' || ev.Rune() == 'Q':
		a.quit()
	case ev.Key() == tcell.KeyUp:
		a.ui.SetBrowserCurrent(a.ui.browserList.GetCurrentItem() - 1)
	case ev.Key() == tcell.KeyDown:
		a.ui.SetBrowserCurrent(a.ui.browserList.GetCurrentItem() + 1)
	case ev.Key() == tcell.KeyPgUp:
		a.ui.SetBrowserCurrent(a.ui.browserList.GetCurrentItem() - 10)
	case ev.Key() == tcell.KeyPgDn:
		a.ui.SetBrowserCurrent(a.ui.browserList.GetCurrentItem() + 10)
	case ev.Key() == tcell.KeyHome:
		a.ui.SetBrowserCurrent(0)
	case ev.Key() == tcell.KeyEnd:
		a.ui.SetBrowserCurrent(len(a.browser.Entries()) - 1)
	case ev.Key() == tcell.KeyEnter:
		a.browserEnter()
	case ev.Key() == tcell.KeyBackspace || ev.Key() == tcell.KeyBackspace2:
		a.browserUp()
	case ev.Key() == tcell.KeyEsc:
		a.browserBack()
	case isShiftA(ev):
		a.browserAddDir()
	case ev.Rune() == 'h' || ev.Rune() == 'H':
		a.showHelp("browser")
	default:
		return ev
	}
	return nil
}

func isShiftA(ev *tcell.EventKey) bool {
	if ev.Key() != tcell.KeyRune {
		return false
	}
	if ev.Rune() == 'A' {
		return true
	}
	return ev.Rune() == 'a' && ev.Modifiers()&tcell.ModShift != 0
}

// --- browser actions ---

func (a *App) openBrowser() {
	a.browser = NewBrowser()
	if err := a.browser.Navigate("."); err != nil {
		a.ui.SetStatus(StateStopped, err.Error())
		return
	}
	a.fromPlayer = true
	a.showBrowser()
}

func (a *App) browserEnter() {
	entries := a.browser.Entries()
	cur := a.ui.browserList.GetCurrentItem()
	if cur < 0 || cur >= len(entries) {
		return
	}
	entry := entries[cur]
	if entry.IsDir {
		if err := a.browser.Navigate(entry.Path); err != nil {
			a.ui.SetBrowserStatus(err.Error())
			return
		}
		a.refreshBrowser()
		return
	}

	var files []string
	if f, ok, err := parsePlaylist(entry.Path); err != nil {
		a.ui.SetBrowserStatus(err.Error())
		return
	} else if ok {
		files = f
	} else {
		files = []string{entry.Path}
	}
	a.addFiles(files)
}

func (a *App) browserUp() {
	parent := filepath.Dir(a.browser.Dir())
	if parent == a.browser.Dir() {
		return
	}
	if err := a.browser.Navigate(parent); err != nil {
		return
	}
	a.refreshBrowser()
}

func (a *App) browserBack() {
	if a.fromPlayer {
		a.fromPlayer = false
		a.showPlayer()
	}
}

func (a *App) browserAddDir() {
	files, err := a.browser.DirAudioFiles()
	if err != nil {
		a.ui.SetBrowserStatus(err.Error())
		return
	}
	if len(files) == 0 {
		a.ui.SetBrowserStatus("No audio files in this directory")
		return
	}
	a.ui.SetBrowserStatus(fmt.Sprintf("Added %d files", len(files)))
	a.addFiles(files)
}

func (a *App) addFiles(files []string) {
	if a.fromPlayer {
		a.player.AppendFiles(files)
		a.fromPlayer = false
		a.showPlayer()
		return
	}
	a.player.SetFiles(files, a.shuffle)
	a.showPlayer()
	a.player.Start()
	a.player.PlayCurrent()
}

func (a *App) showHelp(prev string) {
	a.prevPage = prev
	a.page = "help"
	a.ui.ShowHelp()
	a.ui.ShowPage("help")
}

func (a *App) quit() {
	a.player.Stop()
	a.ui.Stop()
}
