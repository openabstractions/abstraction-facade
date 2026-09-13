//go:build linux || darwin

package bootstrap

import (
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegistrationFIFORefusesWithoutPeer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registration")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := readAgent(path); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(time.Second):
		// Release a blocked old implementation so the red test cleans up.
		fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		if err == nil {
			unix.Close(fd)
		}
		t.Fatal("registration read blocked waiting for FIFO peer")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("regular"), 0600); err != nil {
		t.Fatal(err)
	}
	if b, err := readAgent(path); err != nil || string(b) != "regular" {
		t.Fatal(string(b), err)
	}
}
