package main

import (
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
