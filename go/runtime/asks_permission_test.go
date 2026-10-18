package runtime

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	asks "github.com/openabstractions/abstraction-asks/go"
	askswire "github.com/openabstractions/abstraction-asks/go/abstraction/asks/api"
	askclient "github.com/openabstractions/abstraction-asks/go/client"
	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	rwire "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

func TestRetainedTypedOperatorWorksWithoutAdmissionPreparation(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("fixture native transport has no XPC Program proof")
	}
	book, err := asks.LoadApplicationBook(filepath.Join(t.TempDir(), "questions.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	effect := rwire.PermissionEffect{Subject: rwire.Subject{Account: owner.Uid, Program: identity.CanonicalProgramPath(program)}, Selections: []rwire.PermissionSelection{{Permission: rwire.PermissionKey{Action: "example.resources/read", ResourceProfile: "example.resources/named@1"}, Selector: rwire.ResourceSelector{Kind: rwire.ResourceSelectorKindOne, Target: []byte("owner-canonical-target")}}}}
	p := asks.TypedPermission{Effect: effect, Digest: asks.PermissionEffectDigest(effect), Origin: "service", Display: askswire.PermissionDisplay{Selections: []askswire.PermissionDisplaySelection{{Operation: "Read saved value", Target: "test value", Disclosure: "Returns the raw value"}}}}
	record, outcome, err := book.AskTypedPermissionFrom("trusted-runtime", "test application", "retained", p, listen.Seen{}, asks.Publisher{})
	if err != nil || outcome != "pending" {
		t.Fatalf("admission %+v %s %v", record, outcome, err)
	}
	o := jobOptions(t)
	o.QuestionBook, o.QuestionEndpoint = book, o.JobEndpoint+"-asks"
	o.QuestionOperator = func(context.Context, *identity.Peer) error { return nil }
	_, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	operator, err := m.ResolveAsksPermissionOperator(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.ResolveAsksPermission(ctx, client.Requirements{}); err == nil {
		t.Fatal("absent preparation advertised caller permission admission")
	}
	page, err := operator.ListPermissionsContext(ctx, "", 64)
	if err != nil || len(page.Records) != 1 || page.Records[0].ID != record.ID {
		t.Fatalf("retained page %+v %v", page, err)
	}
	details, err := askclient.ReadPermissionDetailsContext(ctx, operator, owner.Uid, record.ID)
	if err != nil || details.Effect == nil {
		t.Fatalf("retained details %+v %v", details, err)
	}
	answer, err := operator.AnswerPermissionIfPendingContext(ctx, record.ID, "refuse")
	if err != nil || answer.Outcome != askswire.OperatorDecisionOutcomeAnswered || answer.Record == nil || answer.Record.Option != "refuse" {
		t.Fatalf("retained Refuse %+v %v", answer, err)
	}

	effect.Selections[0].Selector.Target = []byte("other-owner-canonical-target")
	p.Effect, p.Digest = effect, asks.PermissionEffectDigest(effect)
	record, outcome, err = book.AskTypedPermissionFrom("trusted-runtime", "test application", "retained-allow", p, listen.Seen{}, asks.Publisher{})
	if err != nil || outcome != "pending" {
		t.Fatalf("second admission %+v %s %v", record, outcome, err)
	}
	answer, err = operator.AnswerPermissionIfPendingContext(ctx, record.ID, "allow")
	if err != nil || answer.Outcome != askswire.OperatorDecisionOutcomeAnswered || answer.Record == nil || answer.Record.Option != "allow" {
		t.Fatalf("retained answer %+v %v", answer, err)
	}
	if _, err = m.ResolveRightsSelectorOperator(ctx, client.Requirements{}); err == nil {
		t.Fatal("absent Rights owner advertised effect completion")
	}
}
