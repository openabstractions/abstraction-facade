package runtime_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go/client"
	"github.com/openabstractions/abstraction-facade/go/grants"
	host "github.com/openabstractions/abstraction-facade/go/runtime"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	logging "github.com/openabstractions/abstraction-logging/go"
	logwire "github.com/openabstractions/abstraction-logging/go/abstraction/logging"
	logclient "github.com/openabstractions/abstraction-logging/go/client"
	logservice "github.com/openabstractions/abstraction-logging/go/service"
	rights "github.com/openabstractions/abstraction-rights/go"
	rwire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
	rightsservice "github.com/openabstractions/abstraction-rights/go/authorization"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
)

func applicationServiceCode(err error) string {
	var service *logwire.ServiceError
	if errors.As(err, &service) {
		return string(service.Code)
	}
	return fmt.Sprint(err)
}

// Use the production owner catalog for both receiving paths. A separately
// constructed test equivalence table could conceal a composition bypass.
func TestApplicationLogSharedRightsBlocksAndLegacyBoundary(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("native XPC runtime candidate supplies Program proof on macOS")
	}
	for _, mode := range []string{"in-process", "ipc"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			catalog, err := grants.SelectorPermissionDefinitions(grants.SelectorServices{ApplicationLogs: true})
			if err != nil {
				t.Fatal(err)
			}
			policy, err := rights.LoadDecisionPolicyWithDefinitions(filepath.Join(t.TempDir(), "rights.json"), []string{host.LogHistoryAction}, catalog)
			if err != nil {
				t.Fatal(err)
			}
			name := fmt.Sprintf("app-shared-rights-%d-%d", os.Getpid(), time.Now().UnixNano())
			o := host.Options{Endpoint: listen.Endpoint(name + "-runtime"), LogEndpoint: listen.Endpoint(name + "-log"), ConfigEndpoint: listen.Endpoint(name + "-config")}
			var decider host.Decider = policy
			key := rwire.PermissionKey{Action: logging.ApplicationLogReadAction, ResourceProfile: logging.ApplicationLogResourceProfile}
			if mode == "ipc" {
				endpoint := o.LogEndpoint + "-rights"
				h, e := rightsservice.Listen(endpoint, policy, func(_ context.Context, peer *identity.Peer, action, resource string) bool {
					process, e := peer.Process.AtLeast(listen.Program.Process)
					return e == nil && process.PID == os.Getpid() && action == host.LogHistoryAction && resource == host.LogHistoryResource
				})
				if e != nil {
					t.Fatal(e)
				}
				if e = h.EnableSelectorEnforcer(func(_ context.Context, peer *identity.Peer, selection rwire.PermissionSelection) bool {
					process, e := peer.Process.AtLeast(listen.Program.Process)
					return e == nil && process.PID == os.Getpid() && selection.Permission == key && catalog.ValidStoredSelection(selection)
				}); e != nil {
					t.Fatal(e)
				}
				done := make(chan error, 1)
				go func() { done <- h.Serve(ctx) }()
				t.Cleanup(func() {
					h.Close()
					select {
					case err := <-done:
						if err != nil {
							t.Error(err)
						}
					case <-time.After(3 * time.Second):
						t.Error("typed Rights host did not drain")
					}
				})
				decider = rightsclient.New(endpoint)
			}
			out, err := logging.OpenRetainedFileSink(filepath.Join(t.TempDir(), "rows"), logging.LogsRetention{MaxUse: 1 << 20})
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			o.Sink = out
			account, err := user.Current()
			if err != nil {
				t.Fatal(err)
			}
			programs := []string{filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")}
			for i, program := range programs {
				if err := os.WriteFile(program, []byte{byte(i)}, 0600); err != nil {
					t.Fatal(err)
				}
				r := logging.Record{Schema: 1, Time: logging.Timestamp{Time: time.Now().UTC()}, Msg: filepath.Base(program)}
				if err := out.WriteAttributed(r, logging.Emitter{Version: 1, Account: account.Uid, Program: program, Key: identity.ProgramKey(program)}); err != nil {
					t.Fatal(err)
				}
			}
			captured := make(chan rwire.Subject, 8)
			p := host.ApplicationHistoryPolicyFromRights(decider)
			o.LogApplicationHistoryPolicy = logservice.ApplicationHistoryPolicy{Fits: p.Fits, Check: func(ctx context.Context, peer *identity.Peer, query logservice.ApplicationHistoryQuery) ([]bool, error) {
				if subject, e := rightsclient.SubjectFromPeer(peer); e == nil {
					select {
					case captured <- subject:
					default:
					}
				}
				return p.Check(ctx, peer, query)
			}}
			o.LogHistoryPolicy = host.HistoryPolicyFromRights(decider)
			service, err := host.Listen(o)
			if err != nil {
				t.Fatal(err)
			}
			run, stop := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- service.Serve(run) }()
			defer func() {
				stop()
				service.Close()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(3 * time.Second):
					t.Error("application facade did not drain")
				}
			}()
			machine := client.New(o.Endpoint)
			reader, err := machine.ResolveLogApplicationReader(ctx, client.Requirements{})
			if err != nil {
				t.Fatal(err)
			}
			legacy, err := machine.ResolveLogReader(ctx, client.Requirements{})
			if err != nil {
				t.Fatal(err)
			}
			all := logwire.ApplicationLogTarget{Selection: logwire.ApplicationLogSelectionAllApps}
			read := func(target logwire.ApplicationLogTarget) (logwire.ApplicationLogPage, error) {
				return reader.ReadContext(ctx, target, "", 8, 65536)
			}
			if page, err := read(all); err != nil || page.Outcome != logwire.HistoryPageOutcomeNotPermitted || len(page.Records) != 0 || page.Next != "" {
				t.Fatalf("ungranted typed read: %+v %v", page, err)
			}
			var subject rwire.Subject
			select {
			case subject = <-captured:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err := policy.Set(subject, host.LogHistoryAction, host.LogHistoryResource, true); err != nil {
				t.Fatal(err)
			}
			if _, err := legacy.ReadContext(ctx, "", 8, 65536); err != nil {
				t.Fatal("legacy permit", err)
			}
			if page, err := read(all); err != nil || page.Outcome != logwire.HistoryPageOutcomeNotPermitted || len(page.Records) != 0 || page.Next != "" {
				t.Fatalf("legacy permit migrated to typed authority: %+v %v", page, err)
			}
			edit := func(selection rwire.PermissionSelection, permit bool) {
				t.Helper()
				revision, _, e := policy.SelectorSnapshotContext(ctx)
				if e != nil {
					t.Fatal(e)
				}
				result, e := policy.SetSelectorRuleContext(ctx, revision, rwire.SelectorRule{Subject: subject, Selection: selection, Permit: permit}, rights.RuleEdit{By: subject, Why: "receiving regression"}, func() error { return nil })
				if e != nil || result.Outcome != rwire.PolicyEditOutcomeApplied {
					t.Fatalf("selector edit: %+v %v", result, e)
				}
			}
			edit(rwire.PermissionSelection{Permission: key, Selector: rwire.ResourceSelector{Kind: rwire.ResourceSelectorKindAll}}, true)
			if err := policy.Set(subject, host.LogHistoryAction, host.LogSourceResource("windows/application"), false); err != nil {
				t.Fatal(err)
			}
			page, err := read(all)
			if err != nil || page.Outcome != logwire.HistoryPageOutcomePage || len(page.Records) != 2 || !page.AtEnd {
				t.Fatalf("exact native-source block entered app collection: %+v %v", page, err)
			}
			requested, err := logclient.ApplicationLogPermissionTarget(programs[1])
			if err != nil {
				t.Fatal(err)
			}
			edit(rwire.PermissionSelection{Permission: key, Selector: rwire.ResourceSelector{Kind: rwire.ResourceSelectorKindOne, Target: requested}}, false)
			page, err = read(all)
			if err != nil || page.Outcome != logwire.HistoryPageOutcomePage || len(page.Records) != 1 || page.Records[0].Msg != "a" || !page.AtEnd {
				t.Fatalf("typed block did not filter all-app batch: %+v %v", page, err)
			}
			if page, err := read(logwire.ApplicationLogTarget{Selection: logwire.ApplicationLogSelectionOneApp, Program: programs[1]}); err != nil || page.Outcome != logwire.HistoryPageOutcomeNotPermitted || len(page.Records) != 0 || page.Next != "" {
				t.Fatalf("blocked selected application: %+v %v", page, err)
			}
			if _, err := legacy.ReadContext(ctx, "", 8, 65536); applicationServiceCode(err) != "forbidden" {
				t.Fatalf("coarse history escaped typed block: %v", err)
			}
			if err := policy.Set(subject, host.LogHistoryAction, host.LogHistoryResource, false); err != nil {
				t.Fatal(err)
			}
			if page, err := read(all); err != nil || page.Outcome != logwire.HistoryPageOutcomeNotPermitted || len(page.Records) != 0 || page.Next != "" {
				t.Fatalf("legacy explicit block escaped typed all selection: %+v %v", page, err)
			}
			if page, err := read(logwire.ApplicationLogTarget{Selection: logwire.ApplicationLogSelectionOneApp, Program: programs[0]}); err != nil || page.Outcome != logwire.HistoryPageOutcomeNotPermitted || len(page.Records) != 0 || page.Next != "" {
				t.Fatalf("legacy explicit block escaped typed concrete target: %+v %v", page, err)
			}
		})
	}
}
