// Package runtime composes existing services behind a runtime-owned catalogue.
// Providers own persistence; resolution itself neither reads configuration nor
// writes logs during bootstrap.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os/user"
	"strconv"
	"sync"
	"sync/atomic"

	asks "github.com/openabstractions/abstraction-asks/go"
	asksservice "github.com/openabstractions/abstraction-asks/go/application"
	casapi "github.com/openabstractions/abstraction-cas/go/api"
	configservice "github.com/openabstractions/abstraction-config/go/service"
	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/bootstrap"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	"github.com/openabstractions/abstraction-job/go/acceptanceprovider"
	logging "github.com/openabstractions/abstraction-logging/go"
	logservice "github.com/openabstractions/abstraction-logging/go/service"
	model "github.com/openabstractions/abstraction-model/go"
	modelservice "github.com/openabstractions/abstraction-model/go/service"
	rights "github.com/openabstractions/abstraction-rights/go"
	rightsservice "github.com/openabstractions/abstraction-rights/go/authorization"
	storage "github.com/openabstractions/abstraction-storage/go"
	storageservice "github.com/openabstractions/abstraction-storage/go/service"
)

// Options is installation/provider configuration, not an application API.
// The caller owns Sink and closes it after Serve returns. A nil Sink leaves
// logging not_ready while independent providers can still start.
type Options struct {
	Endpoint, LogEndpoint, ConfigEndpoint string
	// ConfigStore selects service-owned atomic state; nil uses the native user
	// store. A custom store requires its private key and an explicit notification
	// source to offer observation. Clients never supply these dependencies.
	ConfigStore             casapi.Store
	ConfigUserKey           string
	ConfigObservationSource configservice.ObservationSource
	// ModelRegistry explicitly selects model providers. Nil leaves lookup absent.
	ModelRegistry *model.Registry
	ModelEndpoint string
	// Storage exposes content only through the explicitly supplied digest policy.
	Storage         storage.Store
	StoragePolicy   storageservice.Policy
	StorageEndpoint string
	// QuestionBook is a separately owned application-profile question store.
	QuestionBook     *asks.Book
	QuestionEndpoint string
	// QuestionOperator explicitly authorizes the human-answering integration.
	// Nil keeps operator methods unavailable to application clients.
	QuestionOperator asksservice.AuthorizeOperator
	// RightsPolicy is an explicit decision provider. Enforcer authorization is
	// required to evaluate another receiving service's attributed subject.
	RightsPolicy   *rights.DecisionPolicy
	RightsEnforcer rightsservice.AuthorizeEnforcer
	// RightsOperator explicitly authorizes policy administration; nil omits its contract.
	RightsOperator rightsservice.AuthorizeOperator
	RightsEndpoint string
	// JobRoot enables a private store; ManagedJobs retains its generated owner.
	JobRoot, JobOwner, JobEndpoint string
	JobExecutor                    acceptanceprovider.Executor
	// JobPolicy may narrow same-owner Program-proven access. It cannot grant
	// another OS user or substitute claims for the observed executable path.
	ManagedJobs bool
	JobPolicy   func(*identity.Peer) bool
	// JobMethodPolicy may narrow access per generated method after caller
	// authorization. Nil retains same-owner Program-proven access plus JobPolicy.
	// It supplies no rights-service lookup by itself.
	JobMethodPolicy acceptanceprovider.MethodPolicy
	Sink            logging.Sink
	OnError         func(error)
}

type Host struct {
	resolver            *resolution.Host
	logging             *logservice.Host
	config              *configservice.Host
	configObserverIndex int
	jobs                *jobHost
	model               *modelservice.Host
	modelIndex          int
	storage             *storageservice.Host
	storageIndex        int
	questions           *asksservice.Host
	questionIndex       int
	rights              *rightsservice.Host
	rightsIndex         int
	mu                  sync.Mutex
	candidates          []resolution.Candidate
	onError             func(error)
	started             atomic.Bool
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
	if options.ModelRegistry != nil && options.ModelEndpoint == "" {
		options.ModelEndpoint, err = bootstrap.Endpoint("model-v1")
		if err != nil {
			return Options{}, err
		}
	}
	if options.Storage != nil && options.StorageEndpoint == "" {
		options.StorageEndpoint, err = bootstrap.Endpoint("storage-content-v1")
		if err != nil {
			return Options{}, err
		}
	}
	if options.QuestionBook != nil && options.QuestionEndpoint == "" {
		options.QuestionEndpoint, err = bootstrap.Endpoint("asks-application-v1")
		if err != nil {
			return Options{}, err
		}
	}
	if options.RightsPolicy != nil && options.RightsEndpoint == "" {
		options.RightsEndpoint, err = bootstrap.Endpoint("rights-authorization-v1")
		if err != nil {
			return Options{}, err
		}
	}
	return options, nil
}

// Listen starts independently available providers and the resolver. Provider-local
// startup failures are reported through OnError; their contracts remain not_ready
// or unavailable. Invalid configuration and resolver failure return an error.
func Listen(options Options) (*Host, error) {
	if (options.ConfigStore == nil) != (options.ConfigUserKey == "") {
		return nil, errors.New("runtime: configuration storage requires a provider and private key")
	}
	if options.RightsPolicy == nil && (options.RightsEndpoint != "" || options.RightsEnforcer != nil || options.RightsOperator != nil) {
		return nil, errors.New("runtime: authorization requires an explicit decision policy")
	}
	if options.QuestionBook != nil && !options.QuestionBook.ApplicationProfile() || options.QuestionBook == nil && (options.QuestionEndpoint != "" || options.QuestionOperator != nil) {
		return nil, errors.New("runtime: questions require a separate application-profile book")
	}
	if (options.Storage != nil) != (options.StoragePolicy != nil) || (options.Storage == nil && options.StorageEndpoint != "") {
		return nil, errors.New("runtime: storage requires an explicit provider and content policy")
	}
	jobsConfigured := options.JobRoot != "" || options.JobOwner != "" || options.JobEndpoint != "" || options.JobExecutor != nil || options.ManagedJobs
	if jobsConfigured && (options.JobRoot == "" || (options.JobOwner == "") != options.ManagedJobs) {
		return nil, errors.New("runtime: jobs require a private root and either explicit or managed ownership")
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
	h := &Host{onError: options.OnError, modelIndex: -1, storageIndex: -1, questionIndex: -1, rightsIndex: -1}
	var startupErrors []error
	l, err := logservice.Listen(options.LogEndpoint, options.Sink)
	if err != nil {
		startupErrors = append(startupErrors, fmt.Errorf("runtime logging startup: %w", err))
	} else {
		h.logging = l
		l.OnError = options.OnError
	}
	var c *configservice.Host
	if options.ConfigStore == nil {
		c, err = configservice.Listen(options.ConfigEndpoint)
	} else {
		c, err = configservice.ListenWithStore(options.ConfigEndpoint, options.ConfigStore, options.ConfigUserKey)
	}
	if err != nil {
		startupErrors = append(startupErrors, fmt.Errorf("runtime config startup: %w", err))
	} else {
		h.config = c
		if options.ConfigObservationSource != nil {
			if err = c.EnableObservation(options.ConfigObservationSource); err != nil {
				h.Close()
				return nil, err
			}
		}
		c.OnError = options.OnError
	}
	if jobsConfigured {
		h.jobs, err = listenJobs(options.JobEndpoint, options.JobRoot, options.JobOwner, owner.Uid, options.JobPolicy, options.JobExecutor, options.ManagedJobs)
		if err == nil {
			h.jobs.methodPolicy = options.JobMethodPolicy
		}
		if err != nil {
			// A failed store cannot establish its logical owner. Publish no job
			// reference until that owner and the authenticated listener exist.
			startupErrors = append(startupErrors, fmt.Errorf("runtime jobs startup: %w", err))
		} else {
			h.jobs.onError = options.OnError
		}
	}
	if options.ModelRegistry != nil {
		h.model, err = modelservice.Listen(options.ModelEndpoint, options.ModelRegistry)
		if err != nil {
			startupErrors = append(startupErrors, fmt.Errorf("runtime model startup: %w", err))
		} else {
			h.model.OnError = options.OnError
		}
	}
	if options.Storage != nil {
		h.storage, err = storageservice.Listen(options.StorageEndpoint, options.Storage, options.StoragePolicy)
		if err != nil {
			startupErrors = append(startupErrors, fmt.Errorf("runtime storage startup: %w", err))
		} else {
			h.storage.OnError = options.OnError
		}
	}
	if options.QuestionBook != nil {
		h.questions, err = asksservice.Listen(options.QuestionEndpoint, options.QuestionBook)
		if err != nil {
			startupErrors = append(startupErrors, fmt.Errorf("runtime questions startup: %w", err))
		} else {
			h.questions.OnError = options.OnError
			if options.QuestionOperator != nil {
				if err := h.questions.EnableOperator(options.QuestionOperator); err != nil {
					h.Close()
					return nil, err
				}
			}
		}
	}
	if options.RightsPolicy != nil {
		h.rights, err = rightsservice.Listen(options.RightsEndpoint, options.RightsPolicy, options.RightsEnforcer)
		if err != nil {
			startupErrors = append(startupErrors, fmt.Errorf("runtime rights startup: %w", err))
		} else {
			h.rights.OnError = options.OnError
			if options.RightsOperator != nil {
				if err := h.rights.EnableOperator(options.RightsOperator); err != nil {
					h.Close()
					return nil, err
				}
			}
		}
	}
	for _, registration := range []struct {
		capability, contract, endpoint string
		ready                          bool
	}{
		{"abstraction.logging", "abstraction.logging/sink@1", options.LogEndpoint, h.logging != nil},
		{"abstraction.config", "abstraction.config/reader@1", options.ConfigEndpoint, h.config != nil},
	} {
		h.candidates = append(h.candidates, resolution.Candidate{Ready: registration.ready, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: registration.capability, Contract: registration.contract,
			Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: registration.endpoint,
			Guarantees: []string{},
		}})
	}
	if h.jobs != nil {
		h.candidates = append(h.candidates, h.jobs.candidate(options.JobEndpoint))
		operations := h.jobs.candidate(options.JobEndpoint)
		operations.Reference.Contract = "abstraction.job/operations@1"
		h.candidates = append(h.candidates, operations)
	}
	// Reader and editor share the configuration listener's readiness lifetime.
	editor := h.candidates[1]
	editor.Reference.Contract = "abstraction.config/editor@1"
	h.candidates = append(h.candidates, editor)
	h.configObserverIndex = -1
	if h.config != nil && h.config.ObservationAvailable() {
		observer := h.candidates[1]
		observer.Reference.Contract = "abstraction.config/observer@1"
		observer.Ready = false
		h.configObserverIndex = len(h.candidates)
		h.candidates = append(h.candidates, observer)
	}
	if h.logging != nil && h.logging.HistoryAvailable() {
		history := h.candidates[0]
		history.Reference.Contract = "abstraction.logging/reader@1"
		h.candidates = append(h.candidates, history)
	}
	if h.logging != nil && h.logging.ObservationAvailable() {
		observer := h.candidates[0]
		observer.Reference.Contract = "abstraction.logging/observer@1"
		h.candidates = append(h.candidates, observer)
	}
	if h.jobs != nil {
		inventory := h.jobs.candidate(options.JobEndpoint)
		inventory.Reference.Contract = "abstraction.job/inventory@1"
		h.candidates = append(h.candidates, inventory)
	}
	if options.ModelRegistry != nil {
		h.modelIndex = len(h.candidates)
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.model != nil, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.model", Contract: "abstraction.model/resolver@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.ModelEndpoint, Guarantees: []string{},
		}})
	}
	if options.Storage != nil {
		h.storageIndex = len(h.candidates)
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.storage != nil, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.storage", Contract: "abstraction.storage/content-reader@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.StorageEndpoint, Guarantees: []string{},
		}})
	}
	if options.QuestionBook != nil {
		h.questionIndex = len(h.candidates)
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.questions != nil, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.asks", Contract: "abstraction.asks/application@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.QuestionEndpoint, Guarantees: []string{},
		}})
	}
	if options.QuestionOperator != nil {
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.questions != nil && h.questions.OperatorAvailable(), Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.asks", Contract: "abstraction.asks/operator@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.QuestionEndpoint, Guarantees: []string{},
		}})
	}
	if options.RightsPolicy != nil {
		h.rightsIndex = len(h.candidates)
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.rights != nil, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.rights", Contract: "abstraction.rights/authorization@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.RightsEndpoint, Guarantees: []string{},
		}})
	}
	if options.RightsOperator != nil {
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.rights != nil && h.rights.OperatorAvailable(), Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.rights", Contract: "abstraction.rights/operator@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.RightsEndpoint, Guarantees: []string{},
		}})
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
	if h.configObserverIndex >= 0 {
		h.config.OnObservationStopped = h.refreshConfigObservation
		h.config.PrepareObservation()
		h.refreshConfigObservation()
	}
	if h.rights != nil {
		h.rights.OnStopped = func() { h.stopped(h.rightsIndex, nil) }
	}
	if h.questions != nil {
		h.questions.OnStopped = func() { h.stopped(h.questionIndex, nil) }
	}
	if h.storage != nil {
		h.storage.OnStopped = func() { h.stopped(h.storageIndex, nil) }
	}
	if h.model != nil {
		h.model.OnStopped = func() { h.stopped(h.modelIndex, nil) }
	}
	if l != nil {
		l.OnStopped = func() { h.stopped(0, nil) }
	}
	if c != nil {
		c.OnStopped = func() { h.stopped(1, nil) }
	}
	if h.jobs != nil {
		h.jobs.onStopped = func() { h.stopped(2, nil) }
	}
	if h.onError != nil {
		for _, err := range startupErrors {
			h.onError(err)
		}
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
	if h.model != nil {
		errs = append(errs, h.model.Close())
	}
	if h.storage != nil {
		errs = append(errs, h.storage.Close())
	}
	if h.questions != nil {
		errs = append(errs, h.questions.Close())
	}
	if h.rights != nil {
		errs = append(errs, h.rights.Close())
	}
	return errors.Join(errs...)
}

func (h *Host) stopped(index int, err error) {
	h.mu.Lock()
	// Contracts served by one endpoint share its readiness lifetime.
	endpoint := h.candidates[index].Reference.Endpoint
	for i := range h.candidates {
		if h.candidates[i].Reference.Endpoint == endpoint {
			h.candidates[i].Ready = false
		}
	}
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

func (h *Host) refreshConfigObservation() {
	h.mu.Lock()
	// Read live provider state while holding the catalog update lock. A failure
	// racing startup can never be overwritten by an earlier readiness snapshot.
	h.candidates[h.configObserverIndex].Ready = h.config.ObservationAvailable()
	catalog, err := resolution.New(h.candidates)
	if err == nil {
		err = h.resolver.Update(catalog)
	}
	h.mu.Unlock()
	if err != nil && h.onError != nil {
		h.onError(err)
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
	if h.rights != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(h.rightsIndex, h.rights.Serve(ctx)) }()
	}
	if h.questions != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(h.questionIndex, h.questions.Serve(ctx)) }()
	}
	if h.storage != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(h.storageIndex, h.storage.Serve(ctx)) }()
	}
	if h.model != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(h.modelIndex, h.model.Serve(ctx)) }()
	}
	if h.logging != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(0, h.logging.Serve(ctx)) }()
	}
	if h.config != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(1, h.config.Serve(ctx)) }()
	}
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
