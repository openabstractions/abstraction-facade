package runtime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	asks "github.com/openabstractions/abstraction-asks/go"
	asksservice "github.com/openabstractions/abstraction-asks/go/application"
	askclient "github.com/openabstractions/abstraction-asks/go/client"
	"github.com/openabstractions/abstraction-facade/go/client"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

func TestResolvedQuestionOperatorRetirement(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	path := filepath.Join(t.TempDir(), "questions.json")
	book, err := asks.LoadApplicationBook(path)
	if err != nil {
		t.Fatal(err)
	}
	o := jobOptions(t)
	o.QuestionBook, o.QuestionEndpoint = book, o.JobEndpoint+"-asks"
	var allow atomic.Bool
	o.QuestionOperator = func(ctx context.Context, peer *identity.Peer) error {
		process, err := peer.Process.AtLeast(listen.Program.Process)
		if err != nil || process.PID != os.Getpid() || !allow.Load() {
			return asksservice.ErrOperatorForbidden
		}
		return ctx.Err()
	}
	pending := askclient.ApplicationQuestion{RequestKey: "retire-pending", Key: "download.reach", Slots: map[string]string{"host": "pending.example"}}
	answered := askclient.ApplicationQuestion{RequestKey: "retire-answered", Key: "download.reach", Slots: map[string]string{"host": "answered.example"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var pendingID, answeredID string

	func() {
		_, stop := runJobRuntime(t, o)
		defer stop()
		m := client.New(o.Endpoint)
		app, err := m.ResolveAsks(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		operator, err := m.ResolveAsksOperator(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range []askclient.ApplicationQuestion{pending, answered} {
			admitted, err := app.AskContext(ctx, q)
			if err != nil || admitted.Answer == nil {
				t.Fatalf("admission %+v %v", admitted, err)
			}
			if q.RequestKey == pending.RequestKey {
				pendingID = admitted.Answer.ID
			} else {
				answeredID = admitted.Answer.ID
			}
		}
		refused, err := operator.RetireQuestionContext(ctx, pendingID)
		if err != nil || refused.Outcome.String() != "forbidden" || refused.Record != nil {
			t.Fatalf("unprivileged retirement %+v %v", refused, err)
		}
		if still, err := app.ObserveContext(ctx, pending.RequestKey, 0); err != nil || still.Outcome.String() != "pending" {
			t.Fatalf("refused retirement changed the question %+v %v", still, err)
		}
		allow.Store(true)
		if decision, err := operator.AnswerQuestionContext(ctx, answeredID, "once"); err != nil || decision.Outcome.String() != "answered" {
			t.Fatalf("answer %+v %v", decision, err)
		}
		retired, err := operator.RetireQuestionContext(ctx, pendingID)
		if err != nil || retired.Outcome.String() != "retired" || retired.Record == nil || retired.Record.ID != pendingID || retired.Record.Option != "" {
			t.Fatalf("pending retirement %+v %v", retired, err)
		}
		if gone, err := app.ObserveContext(ctx, pending.RequestKey, 0); err != nil || gone.Outcome.String() != "gone" || gone.Answer != nil {
			t.Fatalf("retired observation %+v %v", gone, err)
		}
		if replayed, err := app.AskContext(ctx, pending); err != nil || replayed.Outcome.String() != "gone" || replayed.Answer != nil {
			t.Fatalf("retired key readmitted %+v %v", replayed, err)
		}
		if again, err := operator.RetireQuestionContext(ctx, pendingID); err != nil || again.Outcome.String() != "retired" || again.Record != nil {
			t.Fatalf("retirement replay %+v %v", again, err)
		}
		if late, err := operator.AnswerQuestionContext(ctx, pendingID, "once"); err != nil || late.Outcome.String() != "unknown" {
			t.Fatalf("answer after retirement %+v %v", late, err)
		}
		if missing, err := operator.RetireQuestionContext(ctx, "never-admitted-question"); err != nil || missing.Outcome.String() != "unknown" {
			t.Fatalf("unknown retirement %+v %v", missing, err)
		}
		if _, err := operator.RetireQuestionContext(ctx, "bad\nid"); err == nil {
			t.Fatal("client sent an invalid retirement ID")
		}
		page, err := operator.ListQuestionsContext(ctx, "", 64)
		if err != nil || page.Outcome.String() != "page" || len(page.Records) != 1 || page.Records[0].ID != answeredID {
			t.Fatalf("history after retirement %+v %v", page, err)
		}
		done, err := operator.RetireQuestionContext(ctx, answeredID)
		if err != nil || done.Outcome.String() != "retired" || done.Record == nil || done.Record.Option != "once" {
			t.Fatalf("answered retirement %+v %v", done, err)
		}
		allow.Store(false)
		if revoked, err := operator.RetireQuestionContext(ctx, answeredID); err != nil || revoked.Outcome.String() != "forbidden" {
			t.Fatalf("revoked retirement %+v %v", revoked, err)
		}
	}()

	allow.Store(true)
	book, err = asks.LoadApplicationBook(path)
	if err != nil {
		t.Fatal(err)
	}
	o.QuestionBook = book
	_, stop := runJobRuntime(t, o)
	defer stop()
	m := client.New(o.Endpoint)
	operator, err := m.ResolveAsksOperator(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	app, err := m.ResolveAsks(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{pendingID, answeredID} {
		if replay, err := operator.RetireQuestionContext(ctx, id); err != nil || replay.Outcome.String() != "retired" || replay.Record != nil {
			t.Fatalf("retirement replay after restart %s %+v %v", id, replay, err)
		}
	}
	for _, q := range []askclient.ApplicationQuestion{pending, answered} {
		if gone, err := app.ObserveContext(ctx, q.RequestKey, 0); err != nil || gone.Outcome.String() != "gone" {
			t.Fatalf("retirement lost after restart %s %+v %v", q.RequestKey, gone, err)
		}
	}
	if page, err := operator.ListQuestionsContext(ctx, "", 64); err != nil || page.Outcome.String() != "page" || len(page.Records) != 0 || !page.Complete {
		t.Fatalf("history after restart %+v %v", page, err)
	}
}
