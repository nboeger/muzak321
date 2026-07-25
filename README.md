# muzak321

A command-line MP3 music player with ncurses file browser and playlist display.

## Dependencies

- Go 1.21+
- PortAudio development library (ALSA backend):

```bash
sudo apt-get install libportaudio2 portaudio19-dev
```

## Build

```bash
go build -o muzak321 .
```

## Usage

```
muzak321 -f <file>                   Play a file, playlist, directory, or glob
muzak321 -s                          Shuffle playback
muzak321                             File browser mode

  -f accepts:
    song.mp3            single MP3 file
    playlist.m3u        M3U playlist (tracks play one by one)
    /path/to/music/     directory (walked recursively for .mp3 / .m3u)
    '*.mp3'             glob pattern (shell wildcards)
    '1_*.mp3'           wildcard matching

Controls:
  Space    Play / Pause
  M        Mute / Unmute
  P/N      Prev / Next track
  Up/Down  Volume
  D        Audio device
  Q        Quit
```

The player screen shows the song list with the current track highlighted.
Volume defaults to 80%. Press **D** to list and select audio output devices.

## Audio requirements

PortAudio on Linux uses ALSA. For audio to work, one of these is needed:

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
