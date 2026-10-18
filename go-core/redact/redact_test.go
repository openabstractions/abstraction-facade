package redact_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/openabstractions/abstraction-facade/go-core/redact"
	"github.com/openabstractions/abstraction-facade/go-core/redact/redacttest"
)

// Every row of the bypass table gives its text, and no secret of the table
// survives in any answer.
func TestLine(t *testing.T) {
	for _, c := range redacttest.Cases {
		got := redact.Line(c.In, 0)
		if got != c.Want {
			t.Errorf("Line(%q) = %q, want %q", c.In, got, c.Want)
		}
		for _, secret := range redacttest.Secrets {
			if strings.Contains(got, secret) {
				t.Errorf("Line(%q) = %q keeps %q", c.In, got, secret)
			}
		}
	}
}

// A limit cuts on a character boundary after redaction, so a cut never
// splits a secret into a part the rules no longer see.
func TestLineLimit(t *testing.T) {
	long := strings.Repeat("\u00e9", 300)
	if got := redact.Line(long, 240); len(got) > 240 || !utf8.ValidString(got) {
		t.Fatalf("Line cut to %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
	secret := strings.Repeat("x", 200) + " token=abcdefghijk"
	if got := redact.Line(secret, 210); strings.Contains(got, "abc") {
		t.Fatalf("Line(%q, 210) = %q keeps the start of the value", secret, got)
	}
	if got := redact.Line("plain", 0); got != "plain" {
		t.Fatalf("Line(plain, 0) = %q", got)
	}
}

// A literal replacement character stays; an invalid byte is a separator.
func TestLineInvalidBytes(t *testing.T) {
	if got := redact.Line("Bearer\xfftok3n", 0); strings.Contains(got, "tok3n") {
		t.Fatalf("an invalid byte joined bearer and its value: %q", got)
	}
	if got := redact.Line("a \ufffd b", 0); got != "a \ufffd b" {
		t.Fatalf("Line kept %q", got)
	}
}
