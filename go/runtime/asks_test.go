package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	asks "github.com/openabstractions/abstraction-asks/go"
	askclient "github.com/openabstractions/abstraction-asks/go/client"
	"github.com/openabstractions/abstraction-facade/go/client"
)

func TestResolvedQuestionsKeepAdmissionAndAnswer(t *testing.T) {
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
	question := askclient.ApplicationQuestion{RequestKey: "permission-request", Key: "download.reach", Slots: map[string]string{"host": "example.com"}}
	var original string
	func() {
		_, stop := runJobRuntime(t, o)
		defer stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, err := client.New(o.Endpoint).ResolveAsks(ctx, client.Requirements{})
		if err != nil {
			t.Fatal(err)
		}
		first, err := c.AskContext(ctx, question)
		if err != nil || first.Outcome != "pending" || first.Answer == nil || first.Answer.Yes {
			t.Fatalf("admission %+v %v", first, err)
		}
		original = first.Answer.Id
		short, cancelWait := context.WithTimeout(ctx, 20*time.Millisecond)
		_, err = c.ObserveContext(short, question.RequestKey, 1000)
		cancelWait()
		if err == nil {
			t.Fatal("waiting budget ignored")
		}
		replay, err := c.AskContext(ctx, question)
		if err != nil || replay.Answer == nil || replay.Answer.Id != original || replay.Outcome != "pending" {
			t.Fatalf("replay %+v %v", replay, err)
		}
		conflicting := question
		conflicting.Slots = map[string]string{"host": "different.example"}
		conflict, err := c.AskContext(ctx, conflicting)
		if err != nil || conflict.Outcome != "conflict" {
			t.Fatalf("conflict %+v %v", conflict, err)
		}
		// The trusted native operator supplies consent independently of the client.
		if _, err := book.Answer(original, "once"); err != nil {
			t.Fatal(err)
		}
	}()
	book, err = asks.LoadApplicationBook(path)
	if err != nil {
		t.Fatal(err)
	}
	o.QuestionBook = book
	h, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	c, err := m.ResolveAsks(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := c.AskContext(ctx, question)
	if err != nil || answer.Outcome != "answered" || answer.Answer == nil || answer.Answer.Id != original || answer.Answer.Option != "once" {
		t.Fatalf("retained answer %+v %v", answer, err)
	}
	if err := book.Forget(original); err != nil {
		t.Fatal(err)
	}
	forgotten, err := c.AskContext(ctx, question)
	if err != nil || forgotten.Outcome != "gone" {
		t.Fatalf("forgotten request %+v %v", forgotten, err)
	}
	h.questions.Close()
	for {
		_, err = m.ResolveAsks(ctx, client.Requirements{})
		if err != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("question readiness remained green")
		case <-time.After(time.Millisecond):
		}
	}
	var refusal *client.BindingError
	if !errors.As(err, &refusal) || refusal.Status != "not_ready" {
		t.Fatalf("stopped questions %v", err)
	}
	if _, err := m.ResolveConfig(ctx, client.Requirements{}); err != nil {
		t.Fatal("questions disabled configuration", err)
	}
}
