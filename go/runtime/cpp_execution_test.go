package runtime

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	downloadserve "github.com/openabstractions/abstraction-download/go/serve"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The fixture owns all provider configuration; the outside application receives
// only a bootstrap endpoint, HTTP source and caller-owned request key.
func TestCppDurableHTTPExecution(t *testing.T) {
	probe := os.Getenv("OA_CPP_EXECUTION_PROBE")
	if probe == "" {
		t.Skip("set OA_CPP_EXECUTION_PROBE to the installed-header C++ job consumer")
	}
	if runtime.GOOS == "darwin" {
		t.Skip("current Program-proof limitation prevents this success claim")
	}
	body := make([]byte, 131079)
	for i := range body {
		body[i] = byte(i)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write(body)
	}))
	defer server.Close()
	options := jobOptions(t)
	options.JobExecutor = downloadserve.HTTPExecution{}
	_, stop := runJobRuntime(t, options)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, probe, "--execute", options.Endpoint, server.URL, "cpp-http-request")
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("C++ execution: %v\n%s", err, stderr.String())
	}
	line := strings.TrimSuffix(strings.TrimSuffix(string(output), "\n"), "\r")
	if !strings.HasPrefix(line, "RESULT ") {
		t.Fatalf("unexpected result prefix %.100q", line)
	}
	got, err := hex.DecodeString(strings.TrimPrefix(line, "RESULT "))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("result bytes differ: decoded=%d expected=%d err=%v", len(got), len(body), err)
	}
	if requests.Load() != 1 {
		t.Fatalf("HTTP request repeated: %d", requests.Load())
	}
}
