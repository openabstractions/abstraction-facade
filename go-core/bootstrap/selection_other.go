//go:build !windows && !linux

package bootstrap

import "context"

func selectInstalled(context.Context) (Selection, error) {
	if err := unsupportedPlatform(); err != nil {
		return Selection{}, err
	}
	return Selection{}, ErrUnsupportedSelection
}
