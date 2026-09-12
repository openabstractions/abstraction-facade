package resolution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

func TestBoundConnection(t *testing.T) {
	var endpoint string
	if runtime.GOOS == "windows" {
		endpoint = fmt.Sprintf(`\\.\pipe\oa-resolve-test-%d`, time.Now().UnixNano())
	} else {
		// Darwin's socket path limit is shorter than many Go test temp paths.
		dir, err := os.MkdirTemp("", "oa-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(dir); err != nil {
				t.Error(err)
			}
		})
		endpoint = filepath.Join(dir, "resolve.sock")
	}
	l, err := listen.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	candidate, request := example()
	catalog, _ := New([]Candidate{candidate})
	done := make(chan error, 1)
	observed := make(chan bool, 1)
	go func() {
		conn, err := l.Accept()
		if err == nil {
			err = HandleConnection(ctx, conn, catalog, func(peer *identity.Peer, ref wire.ServiceReference) bool {
				_, err := peer.User.AtLeast(listen.Program.User)
				observed <- err == nil
				return err == nil && ref.Provider == candidate.Reference.Provider
			})
		}
		done <- err
	}()
	result, err := NewClient(endpoint, 2*time.Second).Resolve(ctx, request)
	if runtime.GOOS == "darwin" {
		if err == nil {
			t.Fatal("caller below Program proof reached resolver")
		}
		select {
		case serverErr := <-done:
			if !errors.Is(serverErr, identity.ErrNotProven) {
				t.Fatalf("expected proof refusal: %v", serverErr)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		select {
		case <-observed:
			t.Fatal("unproven caller reached policy")
		default:
		}
		t.Log("UNPROVEN successful resolution on Darwin: current transport cannot meet Program proof; refusal verified")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != wire.ResolutionStatusResolved {
		t.Fatalf("%+v", result)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if !<-observed {
		t.Fatal("policy did not receive bound caller evidence")
	}
}

func TestRejectMisleadingReference(t *testing.T) {
	candidate, request := example()
	valid := wire.ResolveResult{Status: wire.ResolutionStatusResolved, Reference: &candidate.Reference}
	if err := ValidateResult(request, valid); err != nil {
		t.Fatal(err)
	}
	cases := []wire.ResolveResult{
		{Status: wire.ResolutionStatusResolved},
		{Status: wire.ResolutionStatusForbidden, Reference: &candidate.Reference},
		{Status: "invented"},
	}
	for _, field := range []string{"capability", "contract", "guarantees", "scope", "endpoint", "nul_endpoint"} {
		ref := clone(candidate.Reference)
		switch field {
		case "capability":
			ref.Capability = "other"
		case "contract":
			ref.Contract = "other"
		case "guarantees":
			ref.Guarantees = nil
		case "scope":
			ref.Scope = wire.ScopeAny
		case "endpoint":
			ref.Endpoint = ""
		case "nul_endpoint":
			ref.Endpoint = "selected\x00other"
		}
		cases = append(cases, wire.ResolveResult{Status: wire.ResolutionStatusResolved, Reference: &ref})
	}
	for _, result := range cases {
		if err := ValidateResult(request, result); err == nil {
			t.Fatalf("accepted misleading result %+v", result)
		}
	}
}
