package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
)

// jobHost composes admission and optional execution with the shared transport.
// The semaphore bounds active connections, including incomplete frames. Accepted
// store mutations finish under provider ownership when callers stop waiting.
type jobHost struct {
	listener     listen.Listener
	provider     *acceptanceprovider.Provider
	owner        string
	policy       func(*identity.Peer) bool
	methodPolicy acceptanceprovider.MethodPolicy
	ctx          context.Context
	cancel       context.CancelFunc
	closeOnce    sync.Once
	lifecycle    sync.Mutex
	serving      bool
	closed       bool
	rootLease    io.Closer
	releaseOnce  sync.Once
	workers      sync.WaitGroup
	slots        chan struct{}
	onError      func(error)
	onStopped    func()
	execution    bool
}

func listenJobs(endpoint, root, logicalOwner, osOwner string, policy func(*identity.Peer) bool, executor acceptanceprovider.Executor, managed bool) (*jobHost, error) {
	lease, err := acceptanceprovider.AcquireHost(root)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			lease.Close()
		}
	}()
	var p *acceptanceprovider.Provider
	if managed {
		p, err = acceptanceprovider.OpenManaged(root, executor)
	} else {
		p, err = acceptanceprovider.OpenWithExecutor(root, logicalOwner, executor)
	}
	if err != nil {
		return nil, err
	}
	l, err := listen.Listen(endpoint)
	if err != nil {
		return nil, err
	}
	keep = true
	ctx, cancel := context.WithCancel(context.Background())
	return &jobHost{rootLease: lease, listener: l, provider: p, owner: osOwner, policy: policy, ctx: ctx, cancel: cancel, slots: make(chan struct{}, 64), execution: executor != nil}, nil
}

func (h *jobHost) candidate(endpoint string) resolution.Candidate {
	return resolution.Candidate{Ready: true, Reference: wire.ServiceReference{
		Provider: h.provider.LogicalOwner(), Capability: "abstraction.job", Contract: "abstraction.job/acceptance@1",
		Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: endpoint,
		Guarantees: h.provider.SupportedGuarantees(),
	}}
}

func (h *jobHost) authorize(peer *identity.Peer) (string, error) {
	if peer == nil {
		return "", errors.New("runtime jobs: caller evidence required")
	}
	user, err := peer.User.AtLeast(listen.Program.User)
	if err != nil {
		return "", err
	}
	principal := ""
	switch user.Kind {
	case "windows":
		principal = user.SID
	case "posix":
		if user.UID >= 0 {
			principal = strconv.Itoa(user.UID)
		}
	}
	if principal == "" || principal != h.owner {
		return "", errors.New("runtime jobs: caller belongs to another owner")
	}
	path, err := peer.Path.AtLeast(listen.Program.Path)
	if err != nil || path == "" || !filepath.IsAbs(path) {
		return "", errors.New("runtime jobs: proven absolute program path required")
	}
	path = filepath.Clean(path)
	if h.policy != nil && !h.policy(peer) {
		return "", errors.New("runtime jobs: program refused by policy")
	}
	// Versioned and length-delimited by JSON; neither PID nor request claims enter
	// this namespace. This is observed executable-path identity, not code signing
	// identity. Moving the executable changes its scope; restarting it does not.
	data, _ := json.Marshal([]string{"owner-program@1", user.Kind, principal, path})
	digest := sha256.Sum256(data)
	return "owner-program@1:" + hex.EncodeToString(digest[:]), nil
}

func (h *jobHost) Close() error {
	var err error
	h.closeOnce.Do(func() {
		h.lifecycle.Lock()
		h.closed = true
		serving := h.serving
		h.lifecycle.Unlock()
		if !serving {
			defer h.releaseRoot()
		}
		h.cancel()
		err = h.listener.Close()
		// Remove readiness before any in-flight provider calls are drained.
		if h.onStopped != nil {
			h.onStopped()
		}
	})
	return err
}

func (h *jobHost) releaseRoot() {
	h.releaseOnce.Do(func() {
		if h.provider != nil {
			if err := h.provider.CloseInventory(); err != nil && h.onError != nil {
				h.onError(err)
			}
		}
		if h.rootLease != nil {
			h.rootLease.Close()
		}
	})
}

func (h *jobHost) Serve(ctx context.Context) error {
	h.lifecycle.Lock()
	if h.closed {
		h.lifecycle.Unlock()
		return nil
	}
	if h.serving {
		h.lifecycle.Unlock()
		return errors.New("runtime jobs: host already served")
	}
	h.serving = true
	h.lifecycle.Unlock()
	defer h.releaseRoot()
	stop := context.AfterFunc(ctx, func() { h.Close() })
	defer stop()
	defer h.workers.Wait()
	defer h.Close()
	if h.execution {
		h.workers.Add(1)
		go func() {
			defer h.workers.Done()
			err := h.provider.Execute(h.ctx)
			if h.ctx.Err() == nil {
				if err == nil {
					err = errors.New("runtime jobs: executor stopped")
				}
				if h.onError != nil {
					h.onError(err)
				}
				h.Close()
			}
		}()
	}
	for {
		conn, err := h.listener.Accept()
		if err != nil {
			if h.ctx.Err() != nil || ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case h.slots <- struct{}{}:
		default:
			conn.Close()
			if h.onError != nil {
				h.onError(errors.New("runtime jobs: concurrent connection limit reached"))
			}
			continue
		}
		h.workers.Add(1)
		go func() {
			defer h.workers.Done()
			defer func() { <-h.slots }()
			defer conn.Close()
			request, cancel := context.WithTimeout(h.ctx, 5*time.Second)
			defer cancel()
			err := acceptanceprovider.HandleConnectionWithPolicy(request, conn, h.provider, h.authorize, h.methodPolicy)
			if err != nil && h.onError != nil && h.ctx.Err() == nil {
				h.onError(err)
			}
		}()
	}
}
