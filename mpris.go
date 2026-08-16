package main

import (
	"time"

	"github.com/godbus/dbus/v5"
)

// MPRIS bus identity.
const (
	mprisName   = "org.mpris.MediaPlayer2.muzak321"
	mprisPath   = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	mprisIface  = "org.mpris.MediaPlayer2"
	mprisPlayer = "org.mpris.MediaPlayer2.Player"
)

// mpris exposes the org.mpris.MediaPlayer2 interface on the session bus so
// playerctl/dbus-send can control playback. All DBus errors are ignored by
// design: no session bus (headless) or a name already owned just means no
// remote control.
type mpris struct {
	p *Player
}

// startMPRIS registers the player on the session bus in the calling
// goroutine (call it as `go startMPRIS(...)`).
func startMPRIS(p *Player) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return
	}
	reply, err := conn.RequestName(mprisName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		return
	}
	m := &mpris{p: p}
	conn.ExportMethodTable(map[string]interface{}{
		"Raise": m.Raise,
		"Quit":  m.Quit,
	}, mprisPath, mprisIface)
	conn.ExportMethodTable(map[string]interface{}{
		"Play":      m.Play,
		"Pause":     m.Pause,
		"PlayPause": m.PlayPause,
		"Next":      m.Next,
		"Previous":  m.Previous,
		"Stop":      m.Stop,
	}, mprisPath, mprisPlayer)
	conn.Export(m, mprisPath, "org.freedesktop.DBus.Properties")
}

// playbackStatus maps the player state to an MPRIS PlaybackStatus string.
func (m *mpris) playbackStatus() string {
	switch m.p.State() {
	case StatePlaying:
		return "Playing"
	case StatePaused, StateMuted:
		return "Paused"
	default:
		return "Stopped"
	}
}

// metadata builds the xesam metadata dict for the current track.
func (m *mpris) metadata() map[string]dbus.Variant {
	file := m.p.CurrentFile()
	md := make(map[string]dbus.Variant)
	if file == "" {
		return md
	}
	md["xesam:url"] = dbus.MakeVariant("file://" + file)
	title, artist := trackTitleArtist(file)
	if title == "" {
		title = cleanFileName(file)
	}
	md["xesam:title"] = dbus.MakeVariant(title)
	if artist != "" {
		md["xesam:artist"] = dbus.MakeVariant([]string{artist})
	}
	if _, dur := m.p.Progress(); dur > 0 {
		md["mpris:length"] = dbus.MakeVariant(int64(dur / time.Microsecond))
	}
	return md
}

// Get implements the org.freedesktop.DBus.Properties getter.
func (m *mpris) Get(_ dbus.Sender, iface, prop string) (dbus.Variant, *dbus.Error) {
	switch iface {
	case mprisIface:
		switch prop {
		case "Identity":
			return dbus.MakeVariant("muzak321"), nil
		case "CanQuit", "CanRaise", "CanControl":
			return dbus.MakeVariant(true), nil
		case "HasTrackList":
			return dbus.MakeVariant(false), nil
		case "SupportedUriSchemes":
			return dbus.MakeVariant([]string{"file"}), nil
		case "SupportedMimeTypes":
			return dbus.MakeVariant([]string{}), nil
		}
	case mprisPlayer:
		switch prop {
		case "PlaybackStatus":
			return dbus.MakeVariant(m.playbackStatus()), nil
		case "CanGoNext", "CanGoPrevious", "CanPlay", "CanPause":
			return dbus.MakeVariant(true), nil
		case "CanSeek":
			return dbus.MakeVariant(false), nil
		case "Metadata":
			return dbus.MakeVariant(m.metadata()), nil
		case "Position":
			pos, _ := m.p.Progress()
			return dbus.MakeVariant(int64(pos / time.Microsecond)), nil
		case "Volume":
			return dbus.MakeVariant(m.p.Volume()), nil
		}
	}
	return dbus.Variant{}, &dbus.Error{Name: "org.freedesktop.DBus.Error.InvalidArgs",
		Body: []interface{}{"no such property: " + prop}}
}

// GetAll implements the org.freedesktop.DBus.Properties enumerator.
func (m *mpris) GetAll(_ dbus.Sender, iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != mprisIface && iface != mprisPlayer {
		return nil, &dbus.Error{Name: "org.freedesktop.DBus.Error.InvalidArgs",
			Body: []interface{}{"no such interface: " + iface}}
	}
	props := []string{"Identity", "CanQuit", "CanRaise", "CanControl", "HasTrackList",
		"SupportedUriSchemes", "SupportedMimeTypes"}
	if iface == mprisPlayer {
		props = []string{"PlaybackStatus", "CanGoNext", "CanGoPrevious", "CanPlay",
			"CanPause", "CanSeek", "Metadata", "Position", "Volume"}
	}
	out := make(map[string]dbus.Variant, len(props))
	for _, p := range props {
		v, err := m.Get("", iface, p)
		if err == nil {
			out[p] = v
		}
	}
	return out, nil
}

// Set implements org.freedesktop.DBus.Properties: all properties are read-only.
func (m *mpris) Set(_ dbus.Sender, iface, prop string, _ dbus.Variant) *dbus.Error {
	return &dbus.Error{Name: "org.freedesktop.DBus.Error.PropertyReadOnly",
		Body: []interface{}{prop + " is read-only"}}
}

// --- org.mpris.MediaPlayer2 methods ---

func (m *mpris) Raise() *dbus.Error { return nil }
func (m *mpris) Quit() *dbus.Error  { return nil }

// --- org.mpris.MediaPlayer2.Player methods ---

func (m *mpris) Play() *dbus.Error {
	m.p.Play()
	return nil
}

func (m *mpris) Pause() *dbus.Error {
	m.p.Pause()
	return nil
}

func (m *mpris) PlayPause() *dbus.Error {
	if m.p.State() == StatePlaying {
		m.p.Pause()
	} else {
		m.p.Play()
	}
	return nil
}

func (m *mpris) Next() *dbus.Error {
	m.p.Next()
	return nil
}

func (m *mpris) Previous() *dbus.Error {
	m.p.Previous()
	return nil
}

// Stop pauses and rewinds to the start. The player has no separate stopped
// state that stays resumable, so this approximates MPRIS Stop without
// bricking the playback loop.
func (m *mpris) Stop() *dbus.Error {
	m.p.Pause()
	_ = m.p.SeekTo(0)
	return nil
}
