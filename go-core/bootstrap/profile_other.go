//go:build !windows

package bootstrap

func probeProfileView() (ProfileView, error) { return ProfileView{}, nil }
