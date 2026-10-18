//go:build !windows

package supervision

import (
	"context"
	"time"
)

type Process struct{}

func Start(context.Context, Options) (*Process, error)        { return nil, ErrUnsupported }
func (*Process) PID() int                                     { return 0 }
func (*Process) StartTime() time.Time                         { return time.Time{} }
func (*Process) Stop() error                                  { return ErrUnsupported }
func (*Process) Wait() (int, error)                           { return 0, ErrUnsupported }
func (*Process) Contains(int, time.Time) (bool, error)        { return false, ErrUnsupported }
func (*Process) Exit() (Exit, bool)                           { return Exit{}, false }
func (*Process) Lend(uintptr, uint32) (uintptr, error)        { return 0, ErrUnsupported }
func (*Process) LendTo(uintptr, uint32, int) (uintptr, error) { return 0, ErrUnsupported }
