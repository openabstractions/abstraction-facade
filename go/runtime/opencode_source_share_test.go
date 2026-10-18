package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	logging "github.com/openabstractions/abstraction-logging/go"
	wire "github.com/openabstractions/abstraction-logging/go/abstraction/logging"
	loggingservice "github.com/openabstractions/abstraction-logging/go/service"
	rights "github.com/openabstractions/abstraction-rights/go"
	rightswire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
	"github.com/openabstractions/abstraction-rights/go/authorization"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
)

// This migrated OpenCode integration fixture composes the existing services at
// the facade boundary. Logging still owns native source.read. Rights controls
// the application's distinct disclosure of buffered records to its assistant.
// The resolver response is controlled test routing; both service hosts and the
// generated clients use actual native Program-bound IPC.
type sourceShareFixture struct {
	id       string
	describe atomic.Int32
	read     atomic.Int32
	endCalls atomic.Int32
	entries  []loggingservice.SourceEntry
}

func (f *sourceShareFixture) ID() string { return f.id }
func (f *sourceShareFixture) Describe(context.Context) (wire.SourceDescriptor, error) {
	f.describe.Add(1)
	return wire.SourceDescriptor{ID: f.id, Availability: wire.SourceAvailabilityAvailable,
		Limitations: []wire.SourceLimitation{wire.SourceLimitationRetention, wire.SourceLimitationNativeRedaction}}, nil
}
func (f *sourceShareFixture) CurrentCursor(context.Context) (string, error) {
	f.endCalls.Add(1)
	return "", nil
}
func (f *sourceShareFixture) Read(ctx context.Context, cursor string, _ int64, _ int64) (loggingservice.NativePage, error) {
	f.read.Add(1)
	if err := ctx.Err(); err != nil {
		return loggingservice.NativePage{}, err
	}
	if cursor != "" {
		return loggingservice.NativePage{Outcome: wire.PageOutcomePage, Next: cursor, AtEnd: true}, nil
	}
	return loggingservice.NativePage{Outcome: wire.PageOutcomePage, Entries: append([]loggingservice.SourceEntry(nil), f.entries...), Next: "fixture:1", AtEnd: true}, nil
}

type sourceShareDiscardSink struct{}

func (sourceShareDiscardSink) Write(logging.Record) error { return nil }

func TestOpenCodeWatchUsesNativeIPCAndSourceRights(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation prevents the native IPC test")
	}
	bun := os.Getenv("OA_SOURCE_WATCH_BUN_EXECUTABLE")
	if bun == "" {
		var err error
		bun, err = exec.LookPath("bun")
		if err != nil {
			t.Skip("Bun is unavailable; set OA_SOURCE_WATCH_BUN_EXECUTABLE to run the cross-language fixture")
		}
	}
	library := os.Getenv("ABSTRACTION_IPC_LIBRARY")
	if library == "" || !filepath.IsAbs(library) {
		t.Skip("an absolute ABSTRACTION_IPC_LIBRARY is required for native Bun IPC")
	}
	if _, err := os.Stat(library); err != nil {
		t.Skipf("native IPC library is unavailable: %v", err)
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate source fixture test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".."))
	fixtureScript := filepath.Join(root, "adopters", "opencode", "fixtures", "source-watch-ipc-fixture.mjs")
	if _, err := os.Stat(fixtureScript); err != nil {
		t.Fatalf("missing Bun fixture script: %v", err)
	}
	// Publish the exact packaged declaration after the explicit operator fills
	// its final feature application. This descriptive publication creates no rule.
	declarationFile := filepath.Join(root, "adopters", "opencode", "rights-action-description.template.json")
	declarationBytes, err := os.ReadFile(declarationFile)
	if err != nil {
		t.Fatal(err)
	}
	var declaration struct {
		Action       string             `json:"action"`
		FeatureApp   rightswire.Subject `json:"feature_app"`
		AppLabel     string             `json:"app_label"`
		Description  string             `json:"description"`
		DeclaredRisk string             `json:"declared_risk"`
	}
	decoder := json.NewDecoder(bytes.NewReader(declarationBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&declaration); err != nil {
		t.Fatal(err)
	}
	sourceID := "windows/application"
	if runtime.GOOS == "linux" {
		sourceID = "linux/system-journal"
	}

	for _, mode := range []string{"allow", "deny", "not-granted", "share-denied", "wrong-resource", "wrong-program", "revoke", "outage", "cancel", "partial-error"} {
		t.Run(mode, func(t *testing.T) {
			denied := mode == "deny"
			fixture := &sourceShareFixture{id: sourceID, entries: []loggingservice.SourceEntry{{
				Record: wire.Record{Schema: 1, Time: "2026-09-29T10:11:12.000000Z", Level: 9007199254740993, Msg: "synthetic fixture event", Attrs: map[string]string{
					"source_field": "fixture-value", "API_KEY": "synthetic-secret-field",
				}},
				Producer: &wire.ProducerClaim{Provider: "fixture-provider", Program: "fixture-writer", PID: 9007199254740995},
			}}}
			endpoint := listen.Endpoint(fmt.Sprintf("ls-opencode-watch-%d-%s", os.Getpid(), mode))
			host, err := loggingservice.Listen(endpoint, sourceShareDiscardSink{})
			if err != nil {
				t.Fatal(err)
			}
			var identityObserved atomic.Bool
			if err := host.EnableSourceReader([]loggingservice.SourceAdapter{fixture}, func(_ context.Context, peer *identity.Peer, action, resource string) error {
				if action != loggingservice.SourceReadAction || resource != loggingservice.SourceReadResource(fixture.id) {
					t.Errorf("wrong source authorization %q/%q", action, resource)
				}
				if _, err := peer.Process.AtLeast(identity.ProofKernel); err != nil {
					t.Errorf("OA caller process identity was not kernel-bound: %v", err)
				} else {
					identityObserved.Store(true)
				}
				if denied {
					return errors.New("fixture source permission denied")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			serveDone := make(chan error, 1)
			go func() { serveDone <- host.Serve(ctx) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-serveDone:
					if err != nil {
						t.Errorf("fixture host stopped with error: %v", err)
					}
				case <-time.After(3 * time.Second):
					t.Error("fixture logging host did not stop")
				}
			})
			// The explicitly configured operator is this Go test process. The
			// Bun plugin is an ordinary direct-decision subject, never an operator.
			account, err := user.Current()
			if err != nil {
				t.Fatal(err)
			}
			program, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			publisher, err := rights.NormalizeDecisionSubject(rightswire.Subject{Account: account.Uid, Program: program})
			if err != nil {
				t.Fatal(err)
			}
			subject, err := rights.NormalizeDecisionSubject(rightswire.Subject{Account: account.Uid, Program: bun})
			if err != nil {
				t.Fatal(err)
			}
			policy, err := rights.LoadDecisionPolicy(filepath.Join(t.TempDir(), "rights.json"), []string{loggingservice.SourceReadAction})
			if err != nil {
				t.Fatal(err)
			}
			rightsEndpoint := listen.Endpoint(fmt.Sprintf("rights-opencode-watch-%d-%s", os.Getpid(), mode))
			rightsHost, err := authorization.Listen(rightsEndpoint, policy, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := rightsHost.EnableOperator(func(_ context.Context, peer *identity.Peer) error {
				got, err := rightsclient.SubjectFromPeer(peer)
				if err != nil || got != publisher {
					return authorization.ErrOperatorForbidden
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			rightsDone := make(chan error, 1)
			go func() { rightsDone <- rightsHost.Serve(ctx) }()
			t.Cleanup(func() {
				rightsHost.Close()
				select {
				case err := <-rightsDone:
					if err != nil {
						t.Errorf("fixture Rights host stopped with error: %v", err)
					}
				case <-time.After(3 * time.Second):
					t.Error("fixture Rights host did not stop")
				}
			})
			operator := rightsclient.NewOperator(rightsEndpoint)
			const shareAction = "opencode/log-source.share"
			page, err := operator.ListPolicyContext(ctx, "", 1)
			if err != nil {
				t.Fatal(err)
			}
			description := rightsclient.ActionDescription{Action: declaration.Action, FeatureApp: subject,
				AppLabel: declaration.AppLabel, Description: declaration.Description, DeclaredRisk: declaration.DeclaredRisk}
			if description.Action != shareAction {
				t.Fatalf("packaged declaration action changed: %q", description.Action)
			}
			registered, err := operator.RegisterDescribedActionContext(ctx, page.Revision, description)
			if err != nil || registered.Outcome != rightswire.ActionEditOutcomeApplied ||
				registered.Current == nil || registered.Current.Description == nil || *registered.Current.Description != description ||
				registered.Current.PublishedBy == nil || *registered.Current.PublishedBy != publisher {
				t.Fatal(registered, err)
			}
			published, err := operator.ListPolicyContext(ctx, "", 64)
			if err != nil || published.Outcome != rightswire.PolicyPageOutcomePage || !published.Complete || len(published.Rules) != 0 {
				t.Fatal("description publication created a rule", published, err)
			}
			if mode != "not-granted" {
				rule := rightswire.PolicyRule{Subject: subject, Action: shareAction, Resource: sourceID, Permit: mode != "share-denied"}
				if mode == "wrong-resource" {
					rule.Resource += "/other"
				}
				if mode == "wrong-program" {
					rule.Subject = publisher
				}
				edit, err := operator.SetRuleContext(ctx, registered.Revision, rule)
				if err != nil || edit.Outcome != rightswire.PolicyEditOutcomeApplied {
					t.Fatal(edit, err)
				}
			}
			childCtx, childCancel := context.WithTimeout(ctx, 20*time.Second)
			defer childCancel()
			cmd := exec.CommandContext(childCtx, bun, fixtureScript, endpoint, mode, sourceID)
			cmd.Dir = filepath.Join(root, "adopters", "opencode")
			cmd.Env = append(os.Environ(), "OA_SOURCE_WATCH_FIXTURE_ENDPOINT="+endpoint,
				"OA_SOURCE_WATCH_RIGHTS_ENDPOINT="+rightsEndpoint,
				"OA_SOURCE_WATCH_FEATURE_ACCOUNT="+subject.Account, "OA_SOURCE_WATCH_FEATURE_PROGRAM="+subject.Program,
				"OA_SOURCE_WATCH_PUBLISHER_ACCOUNT="+publisher.Account, "OA_SOURCE_WATCH_PUBLISHER_PROGRAM="+publisher.Program)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			var output, diagnostics bytes.Buffer
			cmd.Stderr = &diagnostics
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				line := scanner.Text()
				fmt.Fprintln(&output, line)
				if line == "OA_SOURCE_WATCH_FIXTURE_MUTATE=revoke" {
					current, err := operator.ListPolicyContext(ctx, "", 1)
					if err != nil {
						t.Error(err)
						break
					}
					edit, err := operator.RevokeRuleContext(ctx, current.Revision, subject, shareAction, sourceID)
					if err != nil || edit.Outcome != rightswire.PolicyEditOutcomeApplied {
						t.Error(edit, err)
						break
					}
					if _, err := io.WriteString(stdin, "changed\n"); err != nil {
						t.Error(err)
					}
				}
				if line == "OA_SOURCE_WATCH_FIXTURE_MUTATE=outage" {
					if err := rightsHost.Close(); err != nil {
						t.Error(err)
					}
					if _, err := io.WriteString(stdin, "changed\n"); err != nil {
						t.Error(err)
					}
				}
			}
			stdin.Close()
			if err := scanner.Err(); err != nil {
				t.Error(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("Bun OpenCode source-watch fixture: %v\n%s\n%s", err, output.Bytes(), diagnostics.Bytes())
			}
			if !bytes.Contains(output.Bytes(), []byte("OA_SOURCE_WATCH_FIXTURE_RESULT=")) {
				t.Fatalf("Bun fixture returned no result marker: %s", output.Bytes())
			}
			if bytes.Contains(output.Bytes(), []byte("synthetic-secret-field")) {
				t.Fatal("tool output exposed a synthetic secret-bearing native attribute")
			}
			if !identityObserved.Load() {
				t.Fatal("source policy never observed a kernel-bound OA caller")
			}
			if denied {
				if fixture.describe.Load() != 0 || fixture.read.Load() != 0 || fixture.endCalls.Load() != 0 {
					t.Fatalf("denied source touched adapter: describe=%d read=%d start-at-end=%d", fixture.describe.Load(), fixture.read.Load(), fixture.endCalls.Load())
				}
			} else {
				want := int32(2)
				if mode == "not-granted" || mode == "share-denied" || mode == "wrong-resource" || mode == "wrong-program" {
					want = 1
				}
				if fixture.describe.Load() != want || fixture.read.Load() != want || fixture.endCalls.Load() != want {
					t.Fatalf("wrong count-only/share source calls: describe=%d read=%d start-at-end=%d want=%d", fixture.describe.Load(), fixture.read.Load(), fixture.endCalls.Load(), want)
				}
			}
		})
	}
}
