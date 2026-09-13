package runtime

import (
	"context"
	"errors"
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

func TestResolvedQuestionOperatorAuthorityAndRestart(t *testing.T) {
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
	question := askclient.ApplicationQuestion{RequestKey: "operator-question", Key: "download.reach", Slots: map[string]string{"host": "example.com"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var id, cursor string
	func() {
		h, stop := runJobRuntime(t, o)
		defer stop()
		m := client.New(o.Endpoint)
		app, err := m.ResolveAsks(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		admitted, err := app.AskContext(ctx, question)
		if err != nil || admitted.Answer == nil {
			t.Fatalf("admission: %+v %v", admitted, err)
		}
		id = admitted.Answer.Id
		operator, err := m.ResolveAsksOperator(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		refused, err := operator.AnswerQuestionContext(ctx, id, "once")
		if err != nil || refused.Outcome != "forbidden" || refused.Record != nil {
			t.Fatalf("unprivileged answer: %+v %v", refused, err)
		}
		hidden, err := operator.ListQuestionsContext(ctx, "", 1)
		if err != nil || hidden.Outcome != "forbidden" || len(hidden.Records) != 0 {
			t.Fatalf("unprivileged history: %+v %v", hidden, err)
		}
		pending, err := app.ObserveContext(ctx, question.RequestKey, 0)
		if err != nil || pending.Outcome != "pending" {
			t.Fatalf("refusal changed answer: %+v %v", pending, err)
		}
		allow.Store(true)
		second := question
		second.RequestKey = "second-question"
		second.Slots = map[string]string{"host": "second.example"}
		if _, err := app.AskContext(ctx, second); err != nil {
			t.Fatal(err)
		}
		page, err := operator.ListQuestionsContext(ctx, "", 1)
		if err != nil || page.Outcome != "page" || len(page.Records) != 1 || page.Next == "" || page.Complete {
			t.Fatalf("bounded history: %+v %v", page, err)
		}
		cursor = page.Next
		decision, err := operator.AnswerQuestionContext(ctx, id, "once")
		if err != nil || decision.Outcome != "answered" || decision.Record == nil {
			t.Fatalf("answer: %+v %v", decision, err)
		}
		changed, err := operator.ListQuestionsContext(ctx, cursor, 1)
		if err != nil || changed.Outcome != "gap" {
			t.Fatalf("changed history: %+v %v", changed, err)
		}
		answered, err := app.ObserveContext(ctx, question.RequestKey, 0)
		if err != nil || answered.Outcome != "answered" || answered.Answer == nil || answered.Answer.Option != "once" {
			t.Fatalf("application answer: %+v %v", answered, err)
		}
		replay, err := operator.AnswerQuestionContext(ctx, id, "once")
		if err != nil || replay.Record == nil || replay.Record.Answered != decision.Record.Answered {
			t.Fatalf("replay: %+v %v", replay, err)
		}
		conflict, err := operator.AnswerQuestionContext(ctx, id, "refuse")
		if err != nil || conflict.Outcome != "conflict" {
			t.Fatalf("conflicting answer: %+v %v", conflict, err)
		}
		allow.Store(false)
		denied, err := operator.ListQuestionsContext(ctx, cursor, 1)
		if err != nil || denied.Outcome != "forbidden" {
			t.Fatalf("revoked history: %+v %v", denied, err)
		}
		h.questions.Close()
		for {
			_, err = m.ResolveAsksOperator(ctx, client.Requirements{})
			if err != nil {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("operator readiness retained")
			case <-time.After(time.Millisecond):
			}
		}
		var refusal *client.BindingError
		if !errors.As(err, &refusal) || refusal.Status != "not_ready" {
			t.Fatalf("stopped operator: %v", err)
		}
		if _, err = m.ResolveConfig(ctx, client.Requirements{}); err != nil {
			t.Fatal("questions disabled config", err)
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
	gap, err := operator.ListQuestionsContext(ctx, cursor, 1)
	if err != nil || gap.Outcome != "gap" {
		t.Fatalf("restart history: %+v %v", gap, err)
	}
	replay, err := operator.AnswerQuestionContext(ctx, id, "once")
	if err != nil || replay.Outcome != "answered" {
		t.Fatalf("restart answer: %+v %v", replay, err)
	}
}
