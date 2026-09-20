package runtime

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	configwire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	rightsclient "github.com/openabstractions/abstraction-rights/go/client"
)

func TestResolvedRightsEnforceConfigEditing(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	home := t.TempDir()
	for _, key := range []string{"HOME", "APPDATA", "XDG_CONFIG_HOME"} {
		t.Setenv(key, home)
	}
	t.Setenv("ProgramData", filepath.Join(home, "machine"))
	eachDecider(t, []string{ConfigEditAction}, func(t *testing.T, f deciderFixture, o Options) {
		policy := f.policy
		enforce := ConfigEditPolicyFromRights(f.decider)
		subjects := make(chan rightsclient.Subject, 8)
		o.ConfigEditPolicy = func(ctx context.Context, peer *identity.Peer) error {
			if subject, e := rightsclient.SubjectFromPeer(peer); e == nil {
				select {
				case subjects <- subject:
				default:
				}
			}
			return enforce(ctx, peer)
		}
		h, stop := runJobRuntime(t, o)
		defer stop()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		m := client.New(o.Endpoint)
		editor, err := m.ResolveConfigEditor(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		initial, err := editor.ReadUserContext(ctx)
		if err != nil {
			t.Fatal("read remains same-account", err)
		}
		values := initial.Values
		values.Off["rights-proof"] = "edited through rights"
		denied, err := editor.ReplaceUserContext(ctx, initial.Revision, values)
		if err != nil || denied.Outcome != configwire.UserReplaceOutcomeForbidden || denied.Snapshot.Revision != "" || len(denied.Snapshot.Values.Off) != 0 {
			t.Fatalf("ungranted edit %+v %v", denied, err)
		}
		if current, err := editor.ReadUserContext(ctx); err != nil || current.Revision != initial.Revision {
			t.Fatalf("denied edit changed settings %+v %v", current, err)
		}
		var subject rightsclient.Subject
		select {
		case subject = <-subjects:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if err := policy.Set(subject, ConfigEditAction, ConfigEditResource, true); err != nil {
			t.Fatal(err)
		}
		applied, err := editor.ReplaceUserContext(ctx, initial.Revision, values)
		if err != nil || applied.Outcome != configwire.UserReplaceOutcomeApplied {
			t.Fatalf("granted edit %+v %v", applied, err)
		}
		reader, err := m.ResolveConfig(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		if effective, err := reader.ReadWithOverridesContext(ctx, configwire.RunOverrides{}); err != nil || effective.Off["rights-proof"] != "edited through rights" {
			t.Fatalf("reader disagrees with authorized edit %+v %v", effective, err)
		}
		if err := policy.Revoke(subject, ConfigEditAction, ConfigEditResource); err != nil {
			t.Fatal(err)
		}
		restore := applied.Snapshot.Values
		delete(restore.Off, "rights-proof")
		if revoked, err := editor.ReplaceUserContext(ctx, applied.Snapshot.Revision, restore); err != nil || revoked.Outcome != configwire.UserReplaceOutcomeForbidden {
			t.Fatalf("revoked edit %+v %v", revoked, err)
		}
		if err := policy.Set(subject, ConfigEditAction, ConfigEditResource, false); err != nil {
			t.Fatal(err)
		}
		if deny, err := editor.ReplaceUserContext(ctx, applied.Snapshot.Revision, restore); err != nil || deny.Outcome != configwire.UserReplaceOutcomeForbidden {
			t.Fatalf("edit under a deny rule %+v %v", deny, err)
		}
		if err := policy.Set(subject, ConfigEditAction, ConfigEditResource, true); err != nil {
			t.Fatal(err)
		}
		f.outage(t, h)
		outage, err := editor.ReplaceUserContext(ctx, applied.Snapshot.Revision, restore)
		if err != nil || outage.Outcome != configwire.UserReplaceOutcomeUnavailable {
			t.Fatalf("decision outage %+v %v", outage, err)
		}
		if current, err := editor.ReadUserContext(ctx); err != nil || current.Revision != applied.Snapshot.Revision {
			t.Fatalf("refused edits changed settings %+v %v", current, err)
		}
	})
}
