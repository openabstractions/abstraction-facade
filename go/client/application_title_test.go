package client

import "testing"

// A title that reads as OpenAbstractions, through case, spacing, look-alike
// letters or compatibility forms, is reserved; another app's title is not.
func TestReservedApplicationTitle(t *testing.T) {
	for title, reserved := range map[string]bool{
		"OpenAbstractions":         true,
		"OpenAbstractions Helper":  true,
		"open abstractions":        true,
		"Open-Abstractions Panel":  true,
		"0penAbstractions":         true,
		"ОpenAbstractions":         true, // Cyrillic O
		"ＯｐｅｎＡｂｓｔｒａｃｔｉｏｎｓ":         true, // full-width
		"Open\u200bAbstractions":   true, // zero-width space
		"Editor":                   false,
		"My OpenAbstractions tool": false,
		"":                         false,
	} {
		if got := ReservedApplicationTitle(title); got != reserved {
			t.Errorf("ReservedApplicationTitle(%q) = %t, want %t", title, got, reserved)
		}
	}
}
