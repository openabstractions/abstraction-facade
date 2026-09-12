// Package runtime composes existing services behind a runtime-owned catalogue.
// Providers own persistence; resolution itself neither reads configuration nor
// writes logs during bootstrap.
package runtime

import (
	"context"
	"errors"
	"os/user"
	"strconv"
	"sync"
	"sync/atomic"

	configservice "github.com/openabstractions/abstraction-config/go/service"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/bootstrap"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	logging "github.com/openabstractions/abstraction-logging/go"
	logservice "github.com/openabstractions/abstraction-logging/go/service"
)

// Options is installation/provider configuration, not an application API.
// The caller owns Sink and closes it after Serve returns.
type Options struct {
	Endpoint, LogEndpoint, ConfigEndpoint string
	// Jobs are enabled only by an explicit private root and stable logical owner.
	// JobEndpoint defaults only when that pair is configured.
	JobRoot, JobOwner, JobEndpoint string
	// JobPolicy may narrow same-owner Program-proven access. It cannot grant
	// another OS user or substitute claims for the observed executable path.
	JobPolicy func(*identity.Peer) bool
	Sink      logging.Sink
	OnError   func(error)
}

type Host struct {
	resolver   *resolution.Host
	logging    *logservice.Host
	config     *configservice.Host
	jobs       *jobHost
	mu         sync.Mutex
	candidates []resolution.Candidate
	onError    func(error)
	started    atomic.Bool
}

// configureEndpoints resolves defaults before any listeners or stores open.
func configureEndpoints(options Options) (Options, error) {
	var err error
	if options.Endpoint == "" {
		options.Endpoint, err = resolution.CheckedDefaultEndpoint()
		if err != nil {
			return Options{}, err
		}
	}
	for _, item := range []struct {
		value   *string
		service string
	}{
		{&options.LogEndpoint, "logging-v1"}, {&options.ConfigEndpoint, "config-v1"},
	} {
		if *item.value == "" {
			*item.value, err = bootstrap.Endpoint(item.service)
			if err != nil {
				return Options{}, err
			}
		}
	}
	if options.JobRoot != "" && options.JobEndpoint == "" {
		options.JobEndpoint, err = bootstrap.Endpoint("job-acceptance-v1")
		if err != nil {
			return Options{}, err
		}
	}
	return options, nil
}

func Listen(options Options) (*Host, error) {
	if options.Sink == nil {
		return nil, errors.New("runtime: logging provider required")
	}
	jobsConfigured := options.JobRoot != "" || options.JobOwner != "" || options.JobEndpoint != ""
	if jobsConfigured && (options.JobRoot == "" || options.JobOwner == "") {
		return nil, errors.New("runtime: jobs require both a private root and logical owner")
	}
	owner, err := user.Current()
	if err != nil {
		return nil, err
	}
	if owner.Uid == "" {
		return nil, errors.New("runtime: owner identity unavailable")
	}
	options, err = configureEndpoints(options)
	if err != nil {
		return nil, err
	}
	l, err := logservice.Listen(options.LogEndpoint, options.Sink)
	if err != nil {
		return nil, err
	}
	c, err := configservice.Listen(options.ConfigEndpoint)
	if err != nil {
		l.Close()
		return nil, err
	}
	h := &Host{logging: l, config: c, onError: options.OnError}
	if jobsConfigured {
		h.jobs, err = listenJobs(options.JobEndpoint, options.JobRoot, options.JobOwner, owner.Uid, options.JobPolicy)
		if err != nil {
			h.Close()
			return nil, err
		}
		h.jobs.onError = options.OnError
	}
	l.OnError = options.OnError
	c.OnError = options.OnError
	for _, registration := range []struct{ capability, contract, endpoint string }{
		{"abstraction.logging", "abstraction.logging/sink@1", options.LogEndpoint},
		{"abstraction.config", "abstraction.config/reader@1", options.ConfigEndpoint},
	} {
		h.candidates = append(h.candidates, resolution.Candidate{Ready: true, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: registration.capability, Contract: registration.contract,
			Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: registration.endpoint,
			Guarantees: []string{},
		}})
	}
	if h.jobs != nil {
		h.candidates = append(h.candidates, h.jobs.candidate(options.JobOwner, options.JobEndpoint))
	}
	catalog, err := resolution.New(h.candidates)
	if err != nil {
		h.Close()
		return nil, err
	}
	h.resolver, err = resolution.Listen(options.Endpoint, catalog, func(peer *identity.Peer, ref wire.ServiceReference) bool {
		if ref.Capability == "abstraction.job" && h.jobs != nil {
			_, err := h.jobs.authorize(peer)
			return err == nil
		}
		observed, err := peer.User.AtLeast(listen.Program.User)
		if err != nil {
			return false
		}
		if observed.Kind == "windows" {
			return observed.SID == owner.Uid
		}
		return strconv.Itoa(observed.UID) == owner.Uid
	})
	if err != nil {
		h.Close()
		return nil, err
	}
	h.resolver.OnError = options.OnError
	l.OnStopped = func() { h.stopped(0, nil) }
	c.OnStopped = func() { h.stopped(1, nil) }
	if h.jobs != nil {
		h.jobs.onStopped = func() { h.stopped(2, nil) }
	}
	return h, nil
}

func (h *Host) Close() error {
	var errs []error
	if h.resolver != nil {
		errs = append(errs, h.resolver.Close())
	}
	if h.logging != nil {
		errs = append(errs, h.logging.Close())
	}
	if h.config != nil {
		errs = append(errs, h.config.Close())
	}
	if h.jobs != nil {
		errs = append(errs, h.jobs.Close())
	}
	return errors.Join(errs...)
}

func (h *Host) stopped(index int, err error) {
	h.mu.Lock()
	h.candidates[index].Ready = false
	catalog, updateErr := resolution.New(h.candidates)
	if updateErr == nil {
		updateErr = h.resolver.Update(catalog)
	}
	h.mu.Unlock()
	if h.onError != nil {
		if err != nil {
			h.onError(err)
		}
		if updateErr != nil {
			h.onError(updateErr)
		}
	}
}

// Serve isolates a stopped capability from the resolver and its sibling. It
// marks the stopped capability not_ready; activation/restart belongs to the host
// lifecycle and is deliberately not an unbounded retry loop here.
func (h *Host) Serve(ctx context.Context) error {
	if !h.started.CompareAndSwap(false, true) {
		return errors.New("runtime: host already served")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); h.stopped(0, h.logging.Serve(ctx)) }()
	go func() { defer workers.Done(); h.stopped(1, h.config.Serve(ctx)) }()
	if h.jobs != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(2, h.jobs.Serve(ctx)) }()
	}
	err := h.resolver.Serve(ctx)
	cancel()
	h.Close()
	workers.Wait()
	return err
}
