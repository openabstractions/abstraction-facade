//go:build linux || darwin

package bootstrap

import (
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
)

// Nonblocking open permits checking the opened object's type without waiting
// for a peer to open a FIFO substituted for the registration file.
func readAgent(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return nil, errors.New("unsupported LaunchAgent file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if len(raw) > 64<<10 {
		return nil, errors.New("LaunchAgent size limit exceeded")
	}
	return raw, err
}
