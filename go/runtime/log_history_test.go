package runtime_test

import (
	"context"
	"fmt"
	client "github.com/openabstractions/abstraction-facade/go/client"
	host "github.com/openabstractions/abstraction-facade/go/runtime"
	"github.com/openabstractions/abstraction-identity/listen"
	logging "github.com/openabstractions/abstraction-logging/go"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestResolvedLogHistory(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	root := t.TempDir()
	for _, key := range []string{"HOME", "APPDATA", "XDG_CONFIG_HOME", "ProgramData"} {
		t.Setenv(key, root)
	}
	sink, err := logging.OpenFileSink(filepath.Join(root, "private-history"))
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	options := host.Options{Endpoint: listen.Endpoint(fmt.Sprintf("lh-r-%d", os.Getpid())), LogEndpoint: listen.Endpoint(fmt.Sprintf("lh-l-%d", os.Getpid())), ConfigEndpoint: listen.Endpoint(fmt.Sprintf("lh-c-%d", os.Getpid())), Sink: sink}
	h, err := host.Listen(options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("runtime did not stop")
		}
	}()
	machine := client.New(options.Endpoint)
	log, err := machine.ResolveLog(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	history, err := machine.ResolveLogReader(ctx, client.Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.LogContext(ctx, 1, "resolved history", map[string]string{"test": "service"}); err != nil {
		t.Fatal(err)
	}
	for {
		page, err := history.ReadContext(ctx, "", 1, 65536)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Records) > 0 {
			if page.Records[0].Msg != "resolved history" || page.Records[0].Attrs["test"] != "service" {
				t.Fatalf("record: %+v", page)
			}
			end, err := history.ReadContext(ctx, page.Next, 1, 65536)
			if err != nil || !end.AtEnd || len(end.Records) != 0 || end.Next != page.Next {
				t.Fatalf("continuation: %+v %v", end, err)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := history.ReadContext(canceled, "", 1, 65536); err == nil {
		t.Fatal("canceled history call succeeded")
	}
	if _, err := history.ReadContext(ctx, "", 1, 65536); err != nil {
		t.Fatal("cancellation poisoned binding", err)
	}
}
