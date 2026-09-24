# Custom Themes

## Problem

muzak321's UI colors (header, borders, spectrum gradient, selection
highlighting, etc.) are hardcoded package-level constants/vars split across
`ui.go` and `spectrum.go`. There is no way for a user to change the color
scheme without editing and recompiling the source. The project currently has
no config-file system at all — only CLI flags (`main.go`).

## Goal

Let a user override any of the app's colors by dropping a plain-text theme
file in their XDG config directory. No file present → today's btop-style
pale/muted palette is used unchanged. Keep the change minimal and dependency-free,
consistent with the project's "simple, minimal" philosophy (CLAUDE.md).

## Non-goals

- No `--theme <path>` flag or multiple named/bundled themes — XDG path only.
- No TOML/YAML/JSON dependency — plain `key = value` text, stdlib-parsed.
- No live-reload of the theme file while the app is running.
- No per-panel theme (e.g. different theme for browser vs player page).

## File location & format

Path: `filepath.Join(os.UserConfigDir(), "muzak321", "theme.conf")`.
`os.UserConfigDir()` resolves `$XDG_CONFIG_HOME` (falling back to
`~/.config`) on Linux, matching XDG convention with no extra code.

If the file does not exist, `loadTheme()` returns immediately and the
built-in defaults (current hardcoded values) are used — this is the normal
case and produces no output.

Format: one `key = #rrggbb` pair per non-blank, non-comment line.
- Lines that are empty after trimming whitespace are skipped.
- Lines whose first non-whitespace character is `#` are treated as
  comments and skipped. (Color values also start with `#`, but a full
  line starting with `#` before any `=` is unambiguous — a `key = #rrggbb`
  line has the `#` after the `=`, not as the first character.)
- Otherwise the line must match `key = #rrggbb` (whitespace around `=`
  tolerated); anything else is a malformed-line warning.

## Theme keys

All 16 colors the UI currently hardcodes become overridable. Defaults below
are today's existing values, so an absent file or absent key is a no-op.

| Key | Default | Currently |
|---|---|---|
| `header_bg` | `#3a3a3a` | `colHeader` |
| `header_fg` | `#c0c0c0` | `colPaleText` |
| `error_bg` | `#a86b6b` | `colError` |
| `bar_fill_bg` | `#2a2a2a` | `colBarFill` (bg half) |
| `bar_fill_fg` | `#6b9b8f` | `colBarFill` (fg half) |
| `accent_amber` | `#c9b46b` | `colAmber` |
| `accent_teal` | `#6b9b9b` | `colTeal` |
| `border_playlist` | `#6b9b9b` | `borderColorPlaylist` |
| `border_coverart` | `#a88bb5` | `borderColorCoverArt` |
| `border_spectrum` | `#8fb08a` | `borderColorSpectrum` |
| `spectrum_low` | `#5fb05f` | gradient v=0 (derived) |
| `spectrum_mid` | `#b0b05f` | gradient v=0.5 (derived) |
| `spectrum_high` | `#b05f5f` | gradient v=1 (derived) |
| `body_bg` | `#000000` | `tcell.ColorBlack` literals |
| `playlist_selected_fg` | `#ffffff` | `tcell.ColorWhite` |
| `playlist_selected_bg` | `#000000` | `tcell.ColorBlack` |
| `browser_selected_fg` | `#000000` | `tcell.ColorBlack` |
| `browser_selected_bg` | `#ffffff` | `tcell.ColorWhite` |

## Implementation approach

Patch the existing package-level vars in place (no new `Theme` struct, no
refactor of call sites):

1. Convert the remaining `const` colors (`colBarFill`, `colAmber`,
   `colTeal`) and the hardcoded `tcell.ColorWhite`/`tcell.ColorBlack`
   selection/body-background literals in `ui.go` to package `var`s with
   their current values as defaults.
2. Add `spectrum_low`/`spectrum_mid`/`spectrum_high` as package vars
   (`tcell.Color` or raw RGB), replacing the `lo`/`hi` local constants in
   `spectrumColor` (`spectrum.go`).
3. New `theme.go`: a `loadTheme()` function that
   - resolves the config path,
   - returns early (no-op) if the file doesn't exist,
   - parses each line, and
   - for each recognized key, overwrites the corresponding var; for an
     unrecognized key or unparseable value, prints
     `warning: theme.conf line N: ...` to stderr and leaves the default in
     place.
   Uses a `map[string]func(tcell.Color)` (or equivalent) built from the
   table above so adding a key later is a one-line addition.
4. Call `loadTheme()` once in `main()`, before `NewUI()` is constructed
   (colors must be finalized before any widget reads them).
5. Rewrite `spectrumColor(v float64)` to linearly interpolate per RGB
   channel across the three keyframes (`spectrum_low` → `spectrum_mid` for
   `v < 0.5`, `spectrum_mid` → `spectrum_high` for `v >= 0.5`). With the
   default keyframe values this reproduces today's exact output (verified:
   the two-branch formula in the current code is mathematically identical
   to a 3-point piecewise lerp with these particular keyframes).
6. Fix the one duplicated hardcoded hex pair at `ui.go:491` (the page
   title bar `"[#c0c0c0:#3a3a3a] muzak321 ..."`) to build the tag string
   from `header_fg`/`header_bg` vars instead of a separate literal, so
   there's a single source of truth.

## Error handling

- Missing file: silent, defaults used.
- Malformed line / unknown key / bad hex value: one-line warning to
  stderr naming the line number, that key keeps its default, startup
  continues. Never fatal.

## Testing

New `theme_test.go`:
- Parser: valid `key = #rrggbb` lines set the right var; comments and
  blank lines are skipped; malformed lines and unknown keys produce a
  warning and leave defaults untouched; missing file is a no-op.
- `spectrumColor`: with default keyframes, output at `v = 0, 0.25, 0.5,
  0.75, 1` matches today's existing formula's output (regression-style
  check that the rewrite is faithful); with custom keyframes, output
  matches a straightforward lerp.

## Documentation

Add a short "Custom Themes" section to the README (or create one if none
exists) listing the file path, format, and the full key table above.
