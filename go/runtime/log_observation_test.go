package runtime

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/openabstractions/abstraction-facade/go/client"
	logging "github.com/openabstractions/abstraction-logging/go"
)

func TestResolvedLogObservation(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	sink, err := logging.OpenFileSink(filepath.Join(t.TempDir(), "history"))
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	o := jobOptions(t)
	o.Sink = sink
	h, stop := runJobRuntime(t, o)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m := client.New(o.Endpoint)
	observer, err := m.ResolveLogObserver(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := m.ResolveLog(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	end, err := observer.ObserveContext(ctx, "", 1, 65536, 0)
	if err != nil || end.Outcome != "page" || !end.AtEnd || len(end.Records) != 0 {
		t.Fatalf("initial end %+v %v", end, err)
	}
	short, stopWait := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = observer.ObserveContext(short, end.Next, 1, 65536, 1000)
	stopWait()
	if err == nil {
		t.Fatal("wait ignored cancellation")
	}
	for _, msg := range []string{"first retained", "second retained"} {
		if err := writer.LogContext(ctx, 1, msg, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"first retained", "second retained"} {
		page, err := observer.ObserveContext(ctx, end.Next, 1, 65536, 1000)
		if err != nil || page.Outcome != "page" || len(page.Records) != 1 || page.Records[0].Msg != want || page.Next == end.Next {
			t.Fatalf("observation %+v %v expected %s", page, err, want)
		}
		end = page
	}
	expired, err := observer.ObserveContext(ctx, end.Next, 1, 65536, 5)
	if err != nil || expired.Outcome != "page" || len(expired.Records) != 0 || expired.Next != end.Next || !expired.AtEnd {
		t.Fatalf("wait expiry %+v %v", expired, err)
	}
	h.logging.Close()
	for {
		_, err = m.ResolveLogObserver(ctx, client.Requirements{})
		if err != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("logging readiness remained green")
		case <-time.After(time.Millisecond):
		}
	}
	if _, err := m.ResolveConfig(ctx, client.Requirements{}); err != nil {
		t.Fatal("logging failure disabled config", err)
	}
}
