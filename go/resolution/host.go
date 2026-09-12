package resolution

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openabstractions/abstraction-identity/listen"
)

const LocalTransport = "oa-framed-local@1"

// DefaultEndpoint is only bootstrap location; it does not assert installation,
// readiness, or server authority. Installation supplies the trusted runtime.
func DefaultEndpoint() string {
	if endpoint := os.Getenv("ABSTRACTION_RUNTIME_ENDPOINT"); endpoint != "" {
		return endpoint
	}
	return listen.Endpoint("runtime-v1")
}

// Host serves runtime-owned registrations. Clients cannot register providers.
// Update replaces a whole immutable snapshot after the runtime changes readiness.
type Host struct {
	listener listen.Listener
	catalog  atomic.Pointer[Catalog]
	policy   PeerPolicy
	ctx      context.Context
	cancel   context.CancelFunc
	once     sync.Once
	workers  sync.WaitGroup
	started  atomic.Bool
	// OnError must be assigned before Serve and may be called concurrently.
	OnError func(error)
}

func Listen(endpoint string, catalog *Catalog, policy PeerPolicy) (*Host, error) {
	if catalog == nil || policy == nil {
		return nil, errors.New("resolution: catalogue and peer policy required")
	}
	l, err := listen.Listen(endpoint)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &Host{listener: l, policy: policy, ctx: ctx, cancel: cancel}
	h.catalog.Store(catalog)
	return h, nil
}

func (h *Host) Update(catalog *Catalog) error {
	if catalog == nil {
		return errors.New("resolution: nil catalogue")
	}
	h.catalog.Store(catalog)
	return nil
}

func (h *Host) Close() error {
	var err error
	h.once.Do(func() { h.cancel(); err = h.listener.Close() })
	return err
}

func (h *Host) Serve(ctx context.Context) error {
	if !h.started.CompareAndSwap(false, true) {
		return errors.New("resolution: host already served")
	}
	stop := context.AfterFunc(ctx, func() { _ = h.Close() })
	defer stop()
	defer h.workers.Wait()
	defer h.Close()
	// Bound active connections, including clients which never send a frame.
	slots := make(chan struct{}, 64)
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			if h.ctx.Err() != nil || ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		case <-h.ctx.Done():
			_ = conn.Close()
			return nil
		default:
			_ = conn.Close()
			continue
		}
		h.workers.Add(1)
		go func() {
			defer h.workers.Done()
			defer func() { <-slots }()
			defer conn.Close()
			callCtx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
			defer cancel()
			if err := HandleConnection(callCtx, conn, h.catalog.Load(), h.policy); err != nil && h.OnError != nil && h.ctx.Err() == nil {
				h.OnError(err)
			}
		}()
	}
}
