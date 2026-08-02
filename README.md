# muzak321

A command-line music player (MP3, FLAC, OGG, WAV) built with [tview](https://github.com/rivo/tview), featuring a file browser, progress bar, and playlist display. It can also stream live Internet radio (SHOUTcast/Icecast MP3) and reads MP3/FLAC/OGG audio tags to show **Title / Artist** while playing.

## Dependencies

- Go 1.21+

It is built on a few great Go packages:

| Package | What it provides |
| --- | --- |
| [tview](https://github.com/rivo/tview) | Terminal UI (player, file browser, playlist) |
| [tcell](https://github.com/gdamore/tcell/v2) | Low-level terminal handling under tview |
| [beep](https://github.com/faiface/beep) | Decoding & playback engine |
| [go-mp3](https://github.com/hajimehoshi/go-mp3) | MP3 decoding (incl. live streams) |
| [tag](https://github.com/dhowden/tag) | MP3/FLAC/OGG metadata for the **Title / Artist** display |

## Build

```bash
go build -o muzak321 .
```

## Usage

```
muzak321 -f <file>                   Play a file, playlist, stream URL, directory, or glob
muzak321 -s                          Shuffle playback
muzak321                             File browser mode

  -f accepts:
    song.mp3            single audio file (mp3, flac, ogg, wav)
    playlist.m3u        M3U playlist (tracks play one by one)
    stations.pls        PLS stream playlist (remote streams + local tracks)
    http://...          live MP3 stream (SHOUTcast/Icecast)
    /path/to/music/     directory (walked recursively for audio / .m3u / .pls)
    '*.flac'            glob pattern (shell wildcards)
    '1_*.mp3'           wildcard matching

Controls:
  Space    Play / Pause
  M        Mute / Unmute
  P / N    Prev / Next track
  Up/Down  Volume
  A        Add songs (opens the file browser)
  H        Help
  Q        Quit

File browser:
  Up/Down     Move
  Enter       Open a directory / add the selected file
  Shift+A     Add every music file in the current directory
  Backspace   Go up one directory
  Esc         Back to the player
  H           Help
  Q           Quit
```

The player screen shows the current track, a live progress bar with elapsed/duration
time, and the playlist with the current track highlighted. Volume defaults to 80%.

For local files, the header shows the audio tag as **Title / Artist** when available
(MP3, FLAC, OGG), falling back to the file name. `.pls` playlists are resolved
relative to the playlist file, so they work with both local tracks and remote URLs.

When a remote stream is playing, the header shows the live **StreamTitle** from the
server's ICY metadata (falling back to a friendly URL name), the progress bar turns
into a **LIVE** indicator with elapsed time, and **Prev** is disabled (a live stream
cannot be rewound). Next and the rest of the controls behave as usual.

Press **A** during playback to open the file browser and add songs to the current
playlist. Browsing a directory does **not** add anything — press **Shift+A** while
a directory is open to add every music file it contains (including playlists).
In the standalone file browser (running `muzak321` with no arguments), selecting a
file or pressing Shift+A starts playback with those tracks.

## Audio requirements

Playback uses beep/Oto, which on Linux talks to ALSA. For audio to work, one of
these is needed:

**Option A — ALSA with PulseAudio/PipeWire (recommended)**

Install an audio server that provides an ALSA compatibility layer:

```bash
# For PulseAudio
sudo apt-get install pulseaudio libasound2-plugins alsa-utils

# For PipeWire
sudo apt-get install pipewire pipewire-pulse pipewire-alsa wireplumber alsa-utils
```

This routes ALSA through a per-user audio server, so no special group is needed.

**Option B — Direct ALSA access**

Add yourself to the `audio` group and log out/in:

```bash
sudo usermod -a -G audio $USER
```

If playback fails, the app shows a diagnostic message indicating the likely cause (permissions, missing hardware, etc.).
