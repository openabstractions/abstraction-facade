package bootstrap

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"path/filepath"
	"runtime"
	"testing"
)

type registeredFixture struct {
	state, location string
	scope           uint32
}

func fixtureQueries(t *testing.T, records []registeredFixture) installationQueries {
	t.Helper()
	return installationQueries{
		related: func(upgrade string, index uint32) (string, error) {
			if upgrade != runtimeUpgradeCode {
				t.Fatal("wrong product family")
			}
			if index >= uint32(len(records)) {
				return "", windows.ERROR_NO_MORE_ITEMS
			}
			return string(rune('A' + index)), nil
		},
		property: func(product string, scope uint32, key string) (string, error) {
			record := records[int(product[0]-'A')]
			if scope != record.scope {
				return "", windows.ERROR_UNKNOWN_PRODUCT
			}
			if key == "State" {
				return record.state, nil
			}
			if key == "InstallLocation" {
				return record.location, nil
			}
			t.Fatalf("unexpected property %q", key)
			return "", nil
		},
	}
}

func TestTrustedInstallationSelection(t *testing.T) {
	ctx := context.Background()
	for _, scope := range []uint32{1, 2, 4} {
		dir := t.TempDir()
		selected, err := selectWindowsInstallation(ctx, "S-1-5-21-1-2-3-1001", `\\.\pipe\test`, fixtureQueries(t, []registeredFixture{{"5", dir, scope}}))
		if err != nil {
			t.Fatal(err)
		}
		if selected.Server.Program != filepath.Join(dir, "tools", "openabstractions.exe") || selected.Server.Principal.SID != "S-1-5-21-1-2-3-1001" {
			t.Fatalf("incorrect runtime account/image: %+v", selected)
		}
	}
	for _, tc := range []struct {
		name    string
		records []registeredFixture
		want    error
	}{
		{"missing", nil, ErrNoTrustedInstallation},
		{"advertised", []registeredFixture{{"1", t.TempDir(), 2}}, ErrNoTrustedInstallation},
		{"conflict", []registeredFixture{{"5", t.TempDir(), 2}, {"5", t.TempDir(), 4}}, ErrAmbiguousInstallation},
		{"relative", []registeredFixture{{"5", "relative", 2}}, ErrNoTrustedInstallation},
		{"remote", []registeredFixture{{"5", `\\server\share`, 2}}, ErrNoTrustedInstallation},
		{"state", []registeredFixture{{"7", t.TempDir(), 2}}, ErrNoTrustedInstallation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := selectWindowsInstallation(ctx, "S-1-5-21-1-2-3-1001", `\\.\pipe\test`, fixtureQueries(t, tc.records))
			if !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
}

func TestTrustedInstallationErrorsAndBudget(t *testing.T) {
	q := fixtureQueries(t, []registeredFixture{{"5", t.TempDir(), 2}})
	q.related = func(string, uint32) (string, error) { return "", windows.ERROR_ACCESS_DENIED }
	if _, err := selectWindowsInstallation(context.Background(), "S-1-5-21-1-2-3-1001", "endpoint", q); !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	q = fixtureQueries(t, []registeredFixture{{"5", t.TempDir(), 2}})
	original := q.related
	q.related = func(s string, i uint32) (string, error) { cancel(); return original(s, i) }
	q.property = func(string, uint32, string) (string, error) {
		t.Fatal("query continued after cancellation")
		return "", nil
	}
	if _, err := selectWindowsInstallation(ctx, "S-1-5-21-1-2-3-1001", "endpoint", q); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestMSIQueryABIReadOnlyMissingProduct(t *testing.T) {
	// Query an unrelated fixed GUID only. No product is installed or changed.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, proc := range []*windows.LazyProc{enumRelatedProc, productInfoProc} {
		if err := proc.Find(); err != nil {
			t.Fatal(err)
		}
	}
	const absent = "{CD5DD73A-65E0-4E81-A5A2-A6D98D4A8F0B}"
	if _, err := relatedProduct(absent, 0); !errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
		t.Fatalf("native related query: %v", err)
	}
	if _, err := installedProperty(absent, 4, "State"); !errors.Is(err, windows.ERROR_UNKNOWN_PRODUCT) {
		t.Fatalf("native product query: %v", err)
	}
}
