package main

import (
	"os"
	"strings"
	"testing"
)

func TestHexToColorValid(t *testing.T) {
	c, err := hexToColor("#3a9bd0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := colorHex(c); got != "3a9bd0" {
		t.Errorf("colorHex(hexToColor(%q)) = %q, want %q", "#3a9bd0", got, "3a9bd0")
	}
}

func TestHexToColorInvalid(t *testing.T) {
	cases := []string{
		"3a9bd0",   // missing '#'
		"#3a9bd",   // 5 digits
		"#3a9bd00", // 7 digits
		"#gggggg",  // non-hex digits
		"",
	}
	for _, s := range cases {
		if _, err := hexToColor(s); err == nil {
			t.Errorf("hexToColor(%q): want error, got nil", s)
		}
	}
}

func withColHeader(t *testing.T, f func()) {
	t.Helper()
	orig := colHeader
	t.Cleanup(func() { colHeader = orig })
	f()
}

func TestApplyThemeReaderValidLine(t *testing.T) {
	withColHeader(t, func() {
		warnings := applyThemeReader(strings.NewReader("header_bg = #112233\n"))
		if len(warnings) != 0 {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
		if got := colorHex(colHeader); got != "112233" {
			t.Errorf("colHeader = %q, want %q", got, "112233")
		}
	})
}

func TestApplyThemeReaderCommentsAndBlanks(t *testing.T) {
	withColHeader(t, func() {
		before := colHeader
		warnings := applyThemeReader(strings.NewReader("\n  \n# a comment\n   # indented comment\n"))
		if len(warnings) != 0 {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
		if colHeader != before {
			t.Errorf("colHeader changed on a comment/blank-only file")
		}
	})
}

func TestApplyThemeReaderWhitespaceTolerant(t *testing.T) {
	withColHeader(t, func() {
		warnings := applyThemeReader(strings.NewReader("   header_bg   =   #445566   \n"))
		if len(warnings) != 0 {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
		if got := colorHex(colHeader); got != "445566" {
			t.Errorf("colHeader = %q, want %q", got, "445566")
		}
	})
}

func TestApplyThemeReaderMalformedLine(t *testing.T) {
	warnings := applyThemeReader(strings.NewReader("not a valid line at all\n"))
	if len(warnings) != 1 || !strings.Contains(warnings[0], "line 1") {
		t.Errorf("warnings = %v, want one warning mentioning line 1", warnings)
	}
}

func TestApplyThemeReaderUnknownKey(t *testing.T) {
	warnings := applyThemeReader(strings.NewReader("boarder_playlist = #112233\n"))
	if len(warnings) != 1 || !strings.Contains(warnings[0], "boarder_playlist") {
		t.Errorf("warnings = %v, want one warning naming the bad key", warnings)
	}
}

func TestApplyThemeReaderMissingHash(t *testing.T) {
	withColHeader(t, func() {
		before := colHeader
		warnings := applyThemeReader(strings.NewReader("header_bg = 3a3a3a\n"))
		if len(warnings) != 1 {
			t.Fatalf("warnings = %v, want exactly one warning", warnings)
		}
		if colHeader != before {
			t.Errorf("colHeader changed despite an invalid value")
		}
	})
}

func TestLoadThemeMissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	before := colHeader
	loadTheme() // must not panic; no theme.conf exists under the temp dir
	if colHeader != before {
		t.Errorf("colHeader changed despite no theme.conf existing")
	}
}

func TestLoadThemeAppliesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(dir+"/muzak321", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/muzak321/theme.conf", []byte("header_bg = #654321\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := colHeader
	t.Cleanup(func() { colHeader = orig })
	loadTheme()
	if got := colorHex(colHeader); got != "654321" {
		t.Errorf("colHeader = %q, want %q", got, "654321")
	}
}

func TestSpectrumColorDefaultPalette(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0.0, "5fb05f"},
		{0.25, "87b05f"},
		{0.5, "b0b05f"},
		{0.75, "b0885f"},
		{1.0, "b05f5f"},
	}
	for _, c := range cases {
		if got := spectrumColor(c.v); got != c.want {
			t.Errorf("spectrumColor(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

func TestSpectrumColorCustomKeyframes(t *testing.T) {
	origLow, origMid, origHigh := spectrumLow, spectrumMid, spectrumHigh
	t.Cleanup(func() { spectrumLow, spectrumMid, spectrumHigh = origLow, origMid, origHigh })

	spectrumLow, _ = hexToColor("#000000")
	spectrumMid, _ = hexToColor("#808080")
	spectrumHigh, _ = hexToColor("#ffffff")

	if got := spectrumColor(0.0); got != "000000" {
		t.Errorf("spectrumColor(0.0) = %q, want %q", got, "000000")
	}
	if got := spectrumColor(1.0); got != "ffffff" {
		t.Errorf("spectrumColor(1.0) = %q, want %q", got, "ffffff")
	}
}
