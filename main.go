package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rthornton128/goncurses"
)

type App struct {
	screen    *Screen
	player    *Player
	files     []string
	shuffle   bool
	browser   *Browser
	inBrowser bool
	running   bool
}

func main() {
	m3uFile := flag.String("f", "", "M3U playlist file")
	help := flag.Bool("h", false, "Show help")
	shuffle := flag.Bool("s", false, "Shuffle playback")
	flag.Parse()

	if *help {
		printHelp()
		return
	}

	player := NewPlayer()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		player.Stop()
		os.Exit(0)
	}()

	if *m3uFile != "" {
		files, err := parseM3U(*m3uFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading playlist: %v\n", err)
			os.Exit(1)
		}
		if len(files) == 0 {
			fmt.Fprintln(os.Stderr, "No files found in playlist")
			os.Exit(1)
		}
		player.SetFiles(files, *shuffle)
		player.Start()
		player.PlayCurrent()
	} else {
		screen, err := NewScreen()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing screen: %v\n", err)
			os.Exit(1)
		}
		defer screen.Close()

		go func() {
			<-sigCh
			screen.Close()
			player.Stop()
			os.Exit(0)
		}()

		browser := NewBrowser()

		app := &App{
			screen:    screen,
			player:    player,
			browser:   browser,
			inBrowser: true,
			running:   true,
		}

		err = browser.Navigate(".")
		if err != nil {
			screen.Close()
			fmt.Fprintf(os.Stderr, "Error reading directory: %v\n", err)
			os.Exit(1)
		}

		app.runBrowser()
		return
	}

	screen, err := NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing screen: %v\n", err)
		os.Exit(1)
	}
	defer screen.Close()

	go func() {
		<-sigCh
		screen.Close()
		player.Stop()
		os.Exit(0)
	}()

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
	fmt.Println("  muzak321 -f <playlist.m3u>   Play an M3U playlist")
	fmt.Println("  muzak321 -s                   Shuffle playback")
	fmt.Println("  muzak321 -h                   Show this help")
	fmt.Println("  muzak321                      File browser mode")
	fmt.Println()
	fmt.Println("Controls:")
	fmt.Println("  Space    Play / Pause")
	fmt.Println("  M        Mute / Unmute")
	fmt.Println("  N        Next track")
	fmt.Println("  Q        Quit")
}

func (a *App) runBrowser() {
	for a.running {
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
				continue
			}
			if done {
				files := a.browser.SelectedFiles()
				if len(files) == 0 {
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

		case goncurses.KEY_BACKSPACE, 127, 8:
			a.browser.Navigate("..")
		}
	}
}

func (a *App) renderPlayer() {
	a.screen.Title(cleanFileName(a.player.CurrentFile()))
	a.screen.DeviceInfo(a.player.DeviceName(), a.player.Volume())
	a.screen.EQData([numEqualizerBars]float64{})
	a.screen.StatusBar(a.player.State(), a.player.CurrentFile())
}

func (a *App) runPlayer() {
	a.screen.Clear()
	a.renderPlayer()
	a.screen.Refresh()

	eqTicker := time.NewTicker(50 * time.Millisecond)
	defer eqTicker.Stop()

	for a.running {
		select {
		case file := <-a.player.fileChanged:
			a.screen.Title(cleanFileName(file))
			a.screen.DeviceInfo(a.player.DeviceName(), a.player.Volume())
			a.screen.EQData([numEqualizerBars]float64{})
			a.screen.StatusBar(a.player.State(), file)
			a.screen.Refresh()

		case state := <-a.player.stateChan:
			_ = state
			errMsg := a.player.Error()
			a.screen.DeviceInfo(a.player.DeviceName(), a.player.Volume())
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

		case data := <-a.player.eqChan:
			a.screen.EQData(data)
			a.screen.StatusBar(a.player.State(), a.player.CurrentFile())
			a.screen.Refresh()

		case <-eqTicker.C:
			a.screen.DeviceInfo(a.player.DeviceName(), a.player.Volume())
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

		case key == 'd' || key == 'D':
			if a.player.DeviceCount() == 0 {
				break
			}
			names := DevicesList()
			sel := a.screen.ShowDevices(names, a.player.DeviceIndex())
			a.player.SetDevice(sel)
			a.screen.Clear()
			a.renderPlayer()
			a.screen.Refresh()
			st := a.player.State()
			if st == StatePlaying || st == StatePaused {
				a.player.Stop()
				a.player.Start()
				a.player.PlayCurrent()
			}

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

		case key == 'h' || key == 'H':
			a.screen.ShowHelp()
			a.screen.Clear()
			a.screen.Refresh()
		}
	}
}
