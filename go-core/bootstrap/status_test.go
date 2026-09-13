package bootstrap

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStatusHelperProcess(t *testing.T) {
	if os.Getenv("OA_BOOTSTRAP_STATUS_HELPER") == "absent-service" {
		os.Exit(1060)
	}
	if os.Getenv("OA_BOOTSTRAP_STATUS_HELPER") != "1" {
		return
	}
	time.Sleep(time.Minute)
	os.Exit(0)
}
func TestStatusCommandBounded(t *testing.T) {
	t.Setenv("OA_BOOTSTRAP_STATUS_HELPER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := statusCommand(ctx, os.Args[0], "-test.run=^TestStatusHelperProcess$")
	if err != context.DeadlineExceeded || time.Since(started) > 2*time.Second {
		t.Fatalf("bounded status: %v %s", err, time.Since(started))
	}
	var out boundedStatusOutput
	if _, err := out.Write([]byte(strings.Repeat("x", (64<<10)+1))); err == nil {
		t.Fatal("unbounded output accepted")
	}
}
func TestCancelledObservationDoesNotProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := ObserveInstalled(ctx)
	if got.State != "unknown" || !strings.Contains(got.Detail, "canceled") {
		t.Fatal(got)
	}
}
