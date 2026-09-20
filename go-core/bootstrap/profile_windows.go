package bootstrap

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func probeProfileView() (ProfileView, error) {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		return ProfileView{}, errors.New("bootstrap: profile view: LOCALAPPDATA is not set")
	}
	return probeProfile(root, func(path string) error {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		return f.Close()
	})
}

// probeProfile creates one uniquely named empty file directly in root, which is
// %LOCALAPPDATA% and always exists, so no folder is created in a package's copy.
// A virtualized create lands in Packages\<family>\LocalCache\Local, where the
// probe then finds it. Both copies are removed.
func probeProfile(root string, create func(string) error) (ProfileView, error) {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return ProfileView{}, fmt.Errorf("bootstrap: profile view: %w", err)
	}
	name := fmt.Sprintf(".oa-profile-%d-%s", os.Getpid(), hex.EncodeToString(nonce))
	probe := filepath.Join(root, name)
	if err := create(probe); err != nil {
		return ProfileView{}, fmt.Errorf("bootstrap: profile view: %w", err)
	}
	defer os.Remove(probe)
	copies, err := filepath.Glob(filepath.Join(root, "Packages", "*", "LocalCache", "Local", name))
	if err != nil {
		return ProfileView{}, fmt.Errorf("bootstrap: profile view: %w", err)
	}
	for _, c := range copies {
		os.Remove(c)
	}
	switch len(copies) {
	case 0:
		return ProfileView{}, nil
	case 1:
		family := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(copies[0]))))
		return ProfileView{Virtualized: true, Family: family}, nil
	default:
		return ProfileView{}, fmt.Errorf("bootstrap: profile view: the probe appears in %d package copies: %s", len(copies), strings.Join(copies, ", "))
	}
}
