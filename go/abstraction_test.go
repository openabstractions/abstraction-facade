package abstraction_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	abstraction "github.com/openabstractions/abstraction-facade/go"
)

func TestPrimaryFacadeAbsenceCreatesNoProviders(t *testing.T) {
	root := t.TempDir()
	endpoint := filepath.Join(root, "absent.sock")
	if runtime.GOOS == "windows" {
		endpoint = fmt.Sprintf(`\\.\pipe\oa-facade-absent-%d`, time.Now().UnixNano())
	}
	t.Setenv("ABSTRACTION_RUNTIME_ENDPOINT", endpoint)
	t.Setenv("ABSTRACTION_STORE", filepath.Join(root, "store"))
	t.Setenv("ABSTRACTION_NAS_STORE", filepath.Join(root, "nas"))
	t.Setenv("ABSTRACTION_LOG", filepath.Join(root, "logs"))
	m := abstraction.Discover()
	for name, call := range map[string]func(context.Context) error{
		"logging": func(ctx context.Context) error { _, err := m.ResolveLog(ctx, abstraction.Requirements{}); return err },
		"config": func(ctx context.Context) error {
			_, err := m.ResolveConfig(ctx, abstraction.Requirements{})
			return err
		},
		"config editor": func(ctx context.Context) error {
			_, err := m.ResolveConfigEditor(ctx, abstraction.Requirements{})
			return err
		},
		"router": func(ctx context.Context) error {
			_, err := m.ResolveRouter(ctx, abstraction.Requirements{})
			return err
		},
		"jobs": func(ctx context.Context) error { _, err := m.ResolveJobs(ctx, abstraction.Requirements{}); return err },
		"operations": func(ctx context.Context) error {
			_, err := m.ResolveJobOperations(ctx, abstraction.Requirements{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			if err := call(ctx); err == nil {
				t.Fatal("missing runtime silently supplied capability")
			}
			expired, finish := context.WithCancel(context.Background())
			finish()
			if err := call(expired); !errors.Is(err, context.Canceled) {
				t.Fatalf("lost canceled waiting context: %v", err)
			}
		})
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("facade created local state: %v", entries)
	}
}
