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
  Q        Quit
```

## Audio requirements

Your user must have access to the audio device. On Linux, add yourself to the `audio` group:

```bash
sudo usermod -a -G audio $USER
```

Then log out and back in for the change to take effect.
