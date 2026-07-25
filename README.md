# muzak321

A command-line MP3 music player with ncurses file browser and real-time equalizer.

## Dependencies

- Go 1.21+
- PortAudio development library:

```bash
sudo apt-get install libportaudio2 portaudio19-dev
```

## Build

```bash
go build -o muzak321 .
```

## Usage

```
muzak321 -f <playlist.m3u>   Play an M3U playlist
muzak321 -s                   Shuffle playback
muzak321                      File browser mode

Controls:
  Space    Play / Pause
  M        Mute / Unmute
  N        Next track
  Up/Down  Volume
  D        Audio device
  Q        Quit
```

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

If playback fails, the app shows a diagnostic message indicating the likely cause (permissions, missing hardware, etc.). Press 'D' to list and select audio devices.
