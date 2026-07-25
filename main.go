package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/rthornton128/goncurses"
)

var resizePending int32

type App struct {
	screen    *Screen
	player    *Player
	files     []string
	shuffle   bool
	browser   *Browser
	inBrowser bool
	running   bool
}

func resolvePath(path string) ([]string, error) {
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
			if ext == ".mp3" || ext == ".m3u" {
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
		} else if ext == ".mp3" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no mp3 files found")
	}
	return result, nil
}

func main() {
	fileArg := flag.String("f", "", "MP3 file, playlist, directory, or glob pattern")
	help := flag.Bool("h", false, "Show help")
	shuffle := flag.Bool("s", false, "Shuffle playback")
	flag.Parse()

	if *help {
		printHelp()
		return
	}

	player := NewPlayer()
	var screen *Screen

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		player.Stop()
		if screen != nil {
			goncurses.End()
		}
		os.Exit(0)
	}()

	winchCh := make(chan os.Signal, 1)
	signal.Notify(winchCh, syscall.SIGWINCH)
	go func() {
		for range winchCh {
			atomic.StoreInt32(&resizePending, 1)
		}
	}()

	if *fileArg != "" {
		allArgs := append([]string{*fileArg}, flag.Args()...)
		var allFiles []string
		for _, arg := range allArgs {
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
		player.Start()
		player.PlayCurrent()
	} else {
		var err error
		screen, err = NewScreen()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing screen: %v\n", err)
			os.Exit(1)
		}
		defer screen.Close()

		browser := NewBrowser()
		app := &App{
			screen:    screen,
			player:    player,
			browser:   browser,
			inBrowser: true,
			running:   true,
		}

		if err = browser.Navigate("."); err != nil {
			screen.Close()
			fmt.Fprintf(os.Stderr, "Error reading directory: %v\n", err)
			os.Exit(1)
		}

		app.runBrowser()
		return
	}

	var err error
	screen, err = NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing screen: %v\n", err)
		os.Exit(1)
	}
	defer screen.Close()

	app := &App{
		screen:  screen,
		player:  player,
		running: true,
	}

	app.runPlayer()

	signal.Stop(sigCh)
	close(sigCh)
}

func printHelp() {
	fmt.Println("muzak321 — MP3 Music Player")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  muzak321 -f <file.m3u|file.mp3>   Play a playlist or MP3 file")
	fmt.Println("  muzak321 -s                        Shuffle playback")
	fmt.Println("  muzak321 -h                        Show this help")
	fmt.Println("  muzak321                           File browser mode")
	fmt.Println()
	fmt.Println("Controls:")
	fmt.Println("  Space    Play / Pause")
	fmt.Println("  M        Mute / Unmute")
	fmt.Println("  P/N      Prev / Next track")
	fmt.Println("  Up/Down  Volume")
	fmt.Println("  Q        Quit")
}

func (a *App) runBrowser() {
	for a.running {
		a.handleResize()
		entries := a.browser.Entries()
		a.screen.Browser(entries, a.browser.Cursor(), a.browser.Scroll(), a.browser.Dir())
		a.screen.Refresh()

		key := a.screen.GetKey()
		switch key {
		case 'q', 'Q', 3, 26:
			a.running = false
			return

		case 'h', 'H':
			a.screen.ShowHelp()
			a.screen.Clear()
			a.screen.Refresh()

		case goncurses.KEY_UP:
			a.browser.CursorUp()

		case goncurses.KEY_DOWN:
			a.browser.CursorDown()

		case goncurses.KEY_ENTER, 13, 10:
			done, err := a.browser.Enter()
			if err != nil {
				a.screen.Message(err.Error())
				a.screen.GetKey()
				continue
			}
			if done {
				files := a.browser.SelectedFiles()
				if len(files) == 0 {
					a.screen.Message("empty playlist")
					a.screen.GetKey()
					continue
				}
				a.player = NewPlayer()
				a.player.SetFiles(files, false)
				a.player.Start()
				a.inBrowser = false
				a.player.PlayCurrent()
				a.runPlayer()
				a.inBrowser = true
			}

		case goncurses.KEY_RESIZE:
			a.screen.Resize()

		case goncurses.KEY_BACKSPACE, 127, 8:
			a.browser.Navigate("..")
		}
	}
}

func (a *App) handleResize() {
	if atomic.SwapInt32(&resizePending, 0) != 0 || a.screen.NeedResize() {
		a.screen.Resize()
	}
}

func (a *App) renderPlayer() {
	a.screen.Title(cleanFileName(a.player.CurrentFile()))
	a.screen.DeviceInfo(a.player.DeviceName(), a.player.Volume())
	a.screen.Playlist(a.player.Files(), a.player.CurrentIndex())
	a.screen.StatusBar(a.player.State(), a.player.CurrentFile())
}

func (a *App) runPlayer() {
	a.screen.Clear()
	a.renderPlayer()
	a.screen.Refresh()

	eqTicker := time.NewTicker(50 * time.Millisecond)
	defer eqTicker.Stop()

	for a.running {
		a.handleResize()
		select {
		case file := <-a.player.fileChanged:
			a.screen.Title(cleanFileName(file))
			a.screen.DeviceInfo(a.player.DeviceName(), a.player.Volume())
			a.screen.Playlist(a.player.Files(), a.player.CurrentIndex())
			a.screen.StatusBar(a.player.State(), file)
			a.screen.Refresh()

		case state := <-a.player.stateChan:
			_ = state
			errMsg := a.player.Error()
			a.screen.DeviceInfo(a.player.DeviceName(), a.player.Volume())
			a.screen.Playlist(a.player.Files(), a.player.CurrentIndex())
			a.screen.StatusBar(state, a.player.CurrentFile(), errMsg)
			a.screen.Refresh()
			if state == StateStopped {
				if errMsg != "" {
					a.screen.StatusBar(state, a.player.CurrentFile(), errMsg+" - press any key")
				} else {
					a.screen.StatusBar(state, a.player.CurrentFile(), "Done - press any key")
				}
				a.screen.Refresh()
				if a.inBrowser {
					a.screen.GetKey()
					time.Sleep(500 * time.Millisecond)
					return
				}
				a.screen.GetKey()
				a.running = false
				return
			}

		case <-eqTicker.C:
			a.screen.DeviceInfo(a.player.DeviceName(), a.player.Volume())
			a.screen.Playlist(a.player.Files(), a.player.CurrentIndex())
			a.screen.StatusBar(a.player.State(), a.player.CurrentFile())
			a.screen.Refresh()

		default:
		}

		key := a.screen.GetKey()
		switch {
		case key == 'q' || key == 'Q' || key == 3 || key == 26:
			a.player.Stop()
			a.running = false
			return

		case key == ' ':
			st := a.player.State()
			switch st {
			case StatePlaying:
				a.player.Pause()
			case StatePaused:
				a.player.Play()
			default:
				a.player.Play()
			}
			a.screen.StatusBar(a.player.State(), a.player.CurrentFile())
			a.screen.Refresh()

		case key == 'm' || key == 'M':
			st := a.player.State()
			if st == StateMuted {
				a.player.Unmute()
			} else {
				a.player.Mute()
			}
			a.screen.StatusBar(a.player.State(), a.player.CurrentFile())
			a.screen.Refresh()

		case key == 'n' || key == 'N':
			a.player.Next()

		case key == goncurses.KEY_UP:
			v := a.player.Volume()
			a.player.SetVolume(v + 0.1)
			a.screen.DeviceInfo(a.player.DeviceName(), a.player.Volume())
			a.screen.StatusBar(a.player.State(), a.player.CurrentFile())
			a.screen.Refresh()

		case key == goncurses.KEY_DOWN:
			v := a.player.Volume()
			a.player.SetVolume(v - 0.1)
			a.screen.DeviceInfo(a.player.DeviceName(), a.player.Volume())
			a.screen.StatusBar(a.player.State(), a.player.CurrentFile())
			a.screen.Refresh()

		case key == 'p' || key == 'P':
			a.player.Previous()

		case key == goncurses.KEY_RESIZE:
			a.screen.Resize()
			a.renderPlayer()
			a.screen.Refresh()

		case key == 'h' || key == 'H':
			a.screen.ShowHelp()
			a.screen.Clear()
			a.screen.Refresh()
		}
	}
}
