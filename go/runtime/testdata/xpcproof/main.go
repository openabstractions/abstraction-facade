// Command xpcproof exercises real resolver, configuration and Rights services.
// It runs only against a disposable launchd job and private state directory.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	cas "github.com/openabstractions/abstraction-cas/go/api"
	config "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-facade/go/client"
	runtime "github.com/openabstractions/abstraction-facade/go/runtime"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	logging "github.com/openabstractions/abstraction-logging/go"
	rights "github.com/openabstractions/abstraction-rights/go"
	rwire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 6 {
		return fmt.Errorf("usage: xpcproof server|allowed|denied|wrong-server PREFIX STATE EXPECTED_SERVER ALLOWED_CLIENT")
	}
	mode, prefix, state, server, allowed := os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	endpoint := func(name string) string { return "xpc:" + prefix + "." + name }
	if mode == "server" {
		if err := os.MkdirAll(state, 0700); err != nil {
			return err
		}
		policy, err := rights.LoadDecisionPolicy(filepath.Join(state, "rights.json"), []string{runtime.ConfigEditAction})
		if err != nil {
			return err
		}
		if err = policy.Set(rwire.Subject{Account: strconv.Itoa(os.Getuid()), Program: allowed}, runtime.ConfigEditAction, runtime.ConfigEditResource, true); err != nil {
			return err
		}
		policy.StateRequired = true
		sink, err := logging.OpenFileSink(filepath.Join(state, "logs.jsonl"))
		if err != nil {
			return err
		}
		defer sink.Close()
		h, err := runtime.Listen(runtime.Options{Endpoint: endpoint("runtime"), LogEndpoint: endpoint("logging"), ConfigEndpoint: endpoint("config"), RightsEndpoint: endpoint("rights"), Sink: sink,
			ConfigStore: cas.BoundedFileStore{MaxBytes: 1 << 20}, ConfigUserKey: filepath.Join(state, "config.json"), ConfigWithoutMachine: true,
			ConfigEditPolicy: runtime.ConfigEditPolicyFromRights(policy), RightsPolicy: policy,
		})
		if err != nil {
			return err
		}
		defer h.Close()
		if errs := h.StartupErrors(); len(errs) > 0 {
			return fmt.Errorf("startup: %v", errs)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err = os.WriteFile(filepath.Join(state, "ready"), []byte("ready"), 0600); err != nil {
			return err
		}
		return h.Serve(ctx)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	expected := listen.ServerExpectation{Principal: identity.User{Kind: "posix", UID: os.Getuid(), GID: -1}, Program: server}
	machine := client.NewVerified(endpoint("runtime"), expected)
	if mode == "installed" {
		machine = client.Discover()
	}
	begin := time.Now()
	editor, err := machine.ResolveConfigEditor(ctx, client.Requirements{})
	if mode == "wrong-server" {
		if err == nil {
			return fmt.Errorf("wrong server accepted")
		}
		fmt.Println("wrong server refused:", err)
		return nil
	}
	if err != nil {
		return err
	}
	before, err := editor.ReadUserContext(ctx)
	if err != nil {
		return err
	}
	values := before.Values
	values.Off["xpc-proof"] = mode + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if mode == "benchmark" {
		setup := time.Since(begin)
		samples := make([]int64, 100)
		begin = time.Now()
		for i := range samples {
			start := time.Now()
			result, err := editor.ReplaceUserContext(ctx, before.Revision, values)
			samples[i] = time.Since(start).Microseconds()
			if err != nil || result.Outcome != config.UserReplaceOutcomeForbidden {
				return fmt.Errorf("benchmark expected Rights refusal: %+v %v", result, err)
			}
		}
		elapsed := time.Since(begin)
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"calls": len(samples), "setup_resolve_read_us": setup.Microseconds(), "median_us": samples[49], "p95_us": samples[94], "p99_us": samples[98], "total_us": elapsed.Microseconds(), "policy": "Rights denied config replacement; fresh authenticated session per call"})
	}
	result, err := editor.ReplaceUserContext(ctx, before.Revision, values)
	if err != nil {
		return err
	}
	after, err := editor.ReadUserContext(ctx)
	if err != nil {
		return err
	}
	if mode == "allowed" {
		if result.Outcome != config.UserReplaceOutcomeApplied || after.Values.Off["xpc-proof"] != values.Off["xpc-proof"] {
			return fmt.Errorf("authorized replacement failed: %+v", result)
		}
	} else if mode == "denied" || mode == "installed" {
		if result.Outcome != config.UserReplaceOutcomeForbidden || after.Revision != before.Revision {
			return fmt.Errorf("denial failed or changed state: %+v", result)
		}
	} else {
		return fmt.Errorf("unknown mode %s", mode)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{"caller": mode, "outcome": result.Outcome.String(), "revision": after.Revision})
}
