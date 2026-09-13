//go:build !windows && !linux

package bootstrap

import "context"

func selectInstalled(context.Context) (Selection, error) { return Selection{}, ErrUnsupportedSelection }
