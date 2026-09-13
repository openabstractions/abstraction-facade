package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"github.com/openabstractions/abstraction-identity/listen"
	"time"
)

var ErrNoTrustedInstallation = errors.New("bootstrap: no trusted runtime installation")
var ErrAmbiguousInstallation = errors.New("bootstrap: multiple runtime installations require explicit selection")
var ErrUnsupportedSelection = errors.New("bootstrap: trusted installation selection is not implemented on this platform")

// Selection holds OS installation evidence independently of resolver replies.
// Server identifies the installed runtime process; it does not authenticate a
// different provider returned by that runtime. Keep selections immutable.
type Selection struct {
	Endpoint string
	Server   listen.ServerExpectation
}

// SelectInstalled reads the current user's registered runtime installation.
// No installation, activation or provider I/O is performed. A missing caller
// deadline receives a two-second budget. Native metadata queries are synchronous;
// cancellation is checked around each query, without claiming interruption of
// an in-progress OS metadata call.
func SelectInstalled(ctx context.Context) (Selection, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	result, err := selectInstalled(ctx)
	if ctx.Err() != nil {
		return Selection{}, ctx.Err()
	}
	if err != nil {
		return Selection{}, fmt.Errorf("select installed runtime: %w", err)
	}
	return result, nil
}
