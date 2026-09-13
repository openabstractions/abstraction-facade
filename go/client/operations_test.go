package client

import (
	"bytes"
	"context"
	"errors"
	api "github.com/openabstractions/abstraction-job/go/abstraction/job/acceptance"
	"io"
	"testing"
)

func observation(id api.RequestIdentity) api.ObservationResult {
	return api.ObservationResult{Outcome: "observed", Snapshot: &api.OperationSnapshot{Receipt: *receipt(id).Receipt, State: "pending", Progress: api.WorkProgress{Done: 2, Total: 1}, Failure: &api.WorkFailure{Classification: "retryable", Message: "last attempt"}}}
}
func chunk(id api.RequestIdentity) api.ResultRead {
	return api.ResultRead{Outcome: "data", Chunk: &api.ResultChunk{Receipt: *receipt(id).Receipt, Offset: 0, Total: 3, Data: []byte{0, 10, 255}, Eof: true}}
}
func TestObservationValidation(t *testing.T) {
	id := api.RequestIdentity{Key: "key", HistoryEpoch: "epoch"}
	c, _, _ := jobsFixture(t)
	if err := c.validateObservation(observation(id), id); err != nil {
		t.Fatal("advisory progress and retry failure", err)
	}
	cases := map[string]func(*api.ObservationResult){
		"missing":               func(r *api.ObservationResult) { r.Snapshot = nil },
		"unknown with snapshot": func(r *api.ObservationResult) { r.Outcome = "unknown" },
		"invalid outcome":       func(r *api.ObservationResult) { r.Outcome = "invented"; r.Snapshot = nil },
		"state":                 func(r *api.ObservationResult) { r.Snapshot.State = "invented" },
		"done":                  func(r *api.ObservationResult) { r.Snapshot.Progress.Done = -1 },
		"total":                 func(r *api.ObservationResult) { r.Snapshot.Progress.Total = -1 },
		"failure":               func(r *api.ObservationResult) { r.Snapshot.Failure.Classification = "invented" },
		"identity":              func(r *api.ObservationResult) { r.Snapshot.Receipt.Identity.Key = "other" },
		"owner":                 func(r *api.ObservationResult) { r.Snapshot.Receipt.LogicalOwner = "other" },
		"guarantee":             func(r *api.ObservationResult) { r.Snapshot.Receipt.AcceptedGuarantees = nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			r := observation(id)
			edit(&r)
			if c.validateObservation(r, id) == nil {
				t.Fatal("accepted invalid observation")
			}
		})
	}
	for _, outcome := range []string{"unknown", "forbidden", "invalid", "definitely_not_accepted"} {
		if err := c.validateObservation(api.ObservationResult{Outcome: outcome}, id); err != nil {
			t.Fatal(err)
		}
	}
}
func TestResultChunkValidation(t *testing.T) {
	id := api.RequestIdentity{Key: "key", HistoryEpoch: "epoch"}
	c, _, _ := jobsFixture(t)
	if err := c.validateRead(chunk(id), id, 0, 3); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*api.ResultRead){
		"missing":              func(r *api.ResultRead) { r.Chunk = nil },
		"not ready with chunk": func(r *api.ResultRead) { r.Outcome = "not_ready" },
		"invalid outcome":      func(r *api.ResultRead) { r.Outcome = "invented"; r.Chunk = nil },
		"wrong offset":         func(r *api.ResultRead) { r.Chunk.Offset = 1 },
		"negative total":       func(r *api.ResultRead) { r.Chunk.Total = -1 },
		"overflow total":       func(r *api.ResultRead) { r.Chunk.Total = 2 },
		"early eof":            func(r *api.ResultRead) { r.Chunk.Total = 4 },
		"missing eof":          func(r *api.ResultRead) { r.Chunk.Eof = false },
		"no progress":          func(r *api.ResultRead) { r.Chunk.Data = nil; r.Chunk.Eof = false },
		"oversize":             func(r *api.ResultRead) { r.Chunk.Data = make([]byte, 65537) },
		"identity":             func(r *api.ResultRead) { r.Chunk.Receipt.Identity.HistoryEpoch = "other" },
		"owner":                func(r *api.ResultRead) { r.Chunk.Receipt.LogicalOwner = "other" },
		"guarantee":            func(r *api.ResultRead) { r.Chunk.Receipt.AcceptedGuarantees = nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			r := chunk(id)
			edit(&r)
			if c.validateRead(r, id, 0, 3) == nil {
				t.Fatal("accepted invalid chunk")
			}
		})
	}
	r := chunk(id)
	r.Chunk.Offset = 3
	r.Chunk.Data = nil
	if err := c.validateRead(r, id, 3, 1); err != nil {
		t.Fatal("EOF read", err)
	}
	r = chunk(id)
	r.Chunk.Total = 0
	r.Chunk.Data = nil
	if err := c.validateRead(r, id, 0, 1); err != nil {
		t.Fatal("empty complete result", err)
	}
	r = chunk(id)
	r.Chunk.Total = 65536
	r.Chunk.Data = make([]byte, 65536)
	if err := c.validateRead(r, id, 0, 65536); err != nil {
		t.Fatal("maximum chunk", err)
	}
	for _, outcome := range []string{"not_ready", "unavailable", "unsupported", "unknown", "forbidden", "invalid"} {
		if err := c.validateRead(api.ResultRead{Outcome: outcome}, id, 0, 3); err != nil {
			t.Fatal(err)
		}
	}
}

type operationHandler struct{ calls int }

func (h *operationHandler) ObserveWork(id api.RequestIdentity) (api.ObservationResult, error) {
	h.calls++
	return observation(id), nil
}
func (h *operationHandler) ReadResult(id api.RequestIdentity, offset, max int64) (api.ResultRead, error) {
	h.calls++
	return chunk(id), nil
}

type operationTransport struct {
	dispatcher api.OperationControlDispatcher
	calls      int
	lost       bool
}

func (t *operationTransport) ExchangeFrameContext(ctx context.Context, frame []byte) ([]byte, error) {
	t.calls++
	response, err := t.dispatcher.ExchangeFrame(frame)
	if t.lost {
		t.lost = false
		return nil, context.DeadlineExceeded
	}
	return response, err
}
func TestOperationMethodsUseGeneratedProtocolAndFreshContexts(t *testing.T) {
	c, _, _ := jobsFixture(t)
	h := &operationHandler{}
	tr := &operationTransport{dispatcher: api.OperationControlDispatcher{Handler: h}}
	c.transport = tr
	id := api.RequestIdentity{Key: "key", HistoryEpoch: "epoch"}
	ctx := context.Background()
	if r, err := c.ObserveWork(ctx, id); err != nil || r.Snapshot == nil {
		t.Fatal(r, err)
	}
	for _, args := range [][2]int64{{-1, 1}, {0, 0}, {0, 65537}} {
		if _, err := c.ReadResult(ctx, id, args[0], args[1]); err == nil {
			t.Fatal("invalid input sent")
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.ReadResult(canceled, id, 0, 3); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if tr.calls != 1 {
		t.Fatal("invalid/canceled calls performed I/O")
	}
	tr.lost = true
	if _, err := c.ReadResult(ctx, id, 0, 3); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if tr.calls != 2 || h.calls != 2 {
		t.Fatal("read retried")
	}
	if r, err := c.ReadResult(context.Background(), id, 0, 3); err != nil || r.Chunk == nil {
		t.Fatal(r, err)
	}
	if c.Endpoint() != "test-endpoint" {
		t.Fatal("binding changed")
	}
}

func TestOlderAcceptanceServiceRefusesOperationsWithoutRediscovery(t *testing.T) {
	c, _, tr := jobsFixture(t)
	_, err := c.ObserveWork(context.Background(), api.RequestIdentity{Key: "key", HistoryEpoch: "epoch"})
	var service *api.ServiceError
	if !errors.As(err, &service) || service.Code != "unknown_service" || tr.calls != 1 {
		t.Fatalf("%v calls=%d", err, tr.calls)
	}
}

type copyHandler struct {
	operationHandler
	calls  int
	change string
}

func (h *copyHandler) ReadResult(id api.RequestIdentity, offset, max int64) (api.ResultRead, error) {
	h.calls++
	r := chunk(id)
	r.Chunk.Offset = offset
	r.Chunk.Total = 65537
	if max != 65536 {
		panic("copy requested wrong bound")
	}
	if offset == 0 {
		r.Chunk.Data = bytes.Repeat([]byte{1}, 65536)
		r.Chunk.Eof = false
	} else {
		r.Chunk.Data = []byte{2}
	}
	switch h.change {
	case "total":
		if offset > 0 {
			r.Chunk.Total++
			r.Chunk.Eof = false
		}
	case "operation":
		if offset > 0 {
			r.Chunk.Receipt.OperationId = "other"
		}
	case "empty":
		r.Chunk.Total = 0
		r.Chunk.Data = nil
		r.Chunk.Eof = true
	case "not_ready":
		r = api.ResultRead{Outcome: "not_ready"}
	}
	return r, nil
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
func TestCopyResult(t *testing.T) {
	id := api.RequestIdentity{Key: "key", HistoryEpoch: "epoch"}
	for _, mode := range []string{"normal", "total", "operation", "empty", "not_ready", "short", "error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			c, _, _ := jobsFixture(t)
			h := &copyHandler{change: mode}
			tr := &operationTransport{dispatcher: api.OperationControlDispatcher{Handler: h}}
			c.transport = tr
			var buf bytes.Buffer
			var dst io.Writer = &buf
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			boom := errors.New("sink failed")
			if mode == "short" {
				dst = writerFunc(func(p []byte) (int, error) { return 7, nil })
			}
			if mode == "error" {
				dst = writerFunc(func(p []byte) (int, error) { return 7, boom })
			}
			if mode == "cancel" {
				dst = writerFunc(func(p []byte) (int, error) { cancel(); return len(p), nil })
			}
			n, err := c.CopyResult(ctx, id, dst)
			switch mode {
			case "normal":
				if err != nil || n != 65537 || buf.Len() != 65537 || buf.Bytes()[65536] != 2 || h.calls != 2 {
					t.Fatal(n, err, h.calls)
				}
			case "empty":
				if err != nil || n != 0 || h.calls != 1 {
					t.Fatal(n, err)
				}
			case "total", "operation":
				var e *api.ServiceError
				if n != 65536 || !errors.As(err, &e) || e.Code != "invalid_result" || buf.Len() != 65536 {
					t.Fatal(n, err)
				}
			case "not_ready":
				var e *api.ServiceError
				if n != 0 || !errors.As(err, &e) || e.Code != mode || h.calls != 1 {
					t.Fatal(n, err)
				}
			case "short":
				if n != 7 || !errors.Is(err, io.ErrShortWrite) || h.calls != 1 {
					t.Fatal(n, err)
				}
			case "error":
				if n != 7 || !errors.Is(err, boom) || h.calls != 1 {
					t.Fatal(n, err)
				}
			case "cancel":
				if n != 65536 || !errors.Is(err, context.Canceled) || h.calls != 1 {
					t.Fatal(n, err, h.calls)
				}
			}
		})
	}
}
