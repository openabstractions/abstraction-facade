// Package runtime composes existing services behind a runtime-owned catalogue.
// Providers own persistence; resolution itself neither reads configuration nor
// writes logs during bootstrap.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os/user"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

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
	router "github.com/openabstractions/abstraction-router/go"
	routerservice "github.com/openabstractions/abstraction-router/go/service"
	storage "github.com/openabstractions/abstraction-storage/go"
	storageservice "github.com/openabstractions/abstraction-storage/go/service"
)

// ErrNoLogSink is the startup error of a composition with no logging Sink.
var ErrNoLogSink = errors.New("runtime logging startup: no logging sink configured; abstraction.logging contracts are not_ready")

// Options is installation/provider configuration, not an application API.
// The caller owns Sink and closes it after Serve returns. A nil Sink registers
// the logging sink, reader and observer contracts not_ready, reports
// ErrNoLogSink through OnError and StartupErrors, and still starts independent
// providers.
type Options struct {
	Endpoint, LogEndpoint, ConfigEndpoint string
	// ConfigStore selects service-owned atomic state; nil uses the native user
	// store. A custom store requires its private key and an explicit notification
	// source to offer observation. Clients never supply these dependencies.
	ConfigStore             casapi.Store
	ConfigUserKey           string
	ConfigObservationSource configservice.ObservationSource
	// ConfigWithoutMachine leaves the administrator's machine rung unread, file
	// and directory, for a runtime isolated from the installation.
	ConfigWithoutMachine bool
	// ConfigEditPolicy narrows user-rung replacement to authorized callers, for
	// example ConfigEditPolicyFromRights. Nil keeps same-account Program proof.
	ConfigEditPolicy configservice.EditPolicy
	// LogHistoryPolicy narrows history reading and observation, for example
	// HistoryPolicyFromRights. Nil keeps same-account Program proof.
	LogHistoryPolicy logservice.HistoryPolicy
	// ModelRegistry explicitly selects model providers. Nil leaves lookup absent.
	ModelRegistry *model.Registry
	ModelEndpoint string
	// ModelPolicy narrows lookup per registry, for example ModelPolicyFromRights.
	ModelPolicy modelservice.LookupPolicy
	// Router explicitly selects a router provider. Nil leaves routing absent.
	Router         *router.Router
	RouterEndpoint string
	// RouterPolicy narrows inventory and routing, for example RouterPolicyFromRights.
	RouterPolicy routerservice.Policy
	// Storage exposes content only through the explicitly supplied digest policy.
	Storage         storage.Store
	StoragePolicy   storageservice.Policy
	StorageEndpoint string
	// StorageWritePolicy adds the writer contract on the storage endpoint. It is
	// separate from StoragePolicy and requires a Local+Writable provider and a
	// positive StorageWriteLimit in bytes.
	StorageWritePolicy storageservice.Policy
	StorageWriteLimit  int64
	// StorageWriteRecordPath is the absolute service-owned file of writer request
	// identities, required with the writer. StorageWriteRecords selects its atomic
	// state provider; nil uses a bounded CAS file store.
	StorageWriteRecordPath string
	StorageWriteRecords    casapi.Store
	// StorageChangesPolicy adds the change-observation contract on the storage
	// endpoint; it authorizes each call, and StoragePolicy filters each object.
	// StorageChangesInterval sets how often a listing provider is polled
	// (default one second); StorageChangesCapacity bounds the journal
	// (default storageservice.DefaultChangeCapacity).
	StorageChangesPolicy   storageservice.Policy
	StorageChangesInterval time.Duration
	StorageChangesCapacity int
	// QuestionBook is a separately owned application-profile question store.
	QuestionBook     *asks.Book
	QuestionEndpoint string
	// QuestionOperator explicitly authorizes the human-answering integration.
	// Nil keeps operator methods unavailable to application clients.
	QuestionOperator asksservice.AuthorizeOperator
	// QuestionAskPolicy decides each application Ask, for example
	// AskPolicyFromRights. Nil keeps same-account Program proof.
	QuestionAskPolicy asksservice.AskPolicy
	// RightsPolicy is an explicit decision provider. Enforcer authorization is
	// required to evaluate another receiving service's attributed subject.
	RightsPolicy   *rights.DecisionPolicy
	RightsEnforcer rightsservice.AuthorizeEnforcer
	// RightsOperator explicitly authorizes policy administration; nil omits its contract.
	RightsOperator rightsservice.AuthorizeOperator
	RightsEndpoint string
	// RightsActions are registered into RightsPolicy before the decision service
	// starts, with this host's account and executable as registrant. Registering
	// an action already in the catalogue writes nothing; a failed registration
	// leaves the decision service not ready. ResourceRightsActions lists the
	// actions of the resource services this runtime composes.
	RightsActions []string
	// Credentials explicitly adds a credentials host; nil omits its contracts.
	Credentials         CredentialsService
	CredentialsEndpoint string
	// ModelCredentials lets a model Ref name a credential the lookup applies for
	// the bound caller; nil refuses such a Ref as unsupported_mapping.
	ModelCredentials modelservice.CredentialApplier
	// Inference explicitly adds an inference host; nil omits its contract.
	Inference               InferenceService
	InferenceEndpoint       string
	InferenceRemoteEndpoint string
	// Providers adds the declared providers' candidates after the runtime's
	// own; nil adds none.
	Providers DeclaredProviders
	// Registry explicitly adds the provider registry profile; nil omits it.
	Registry         RegistryService
	RegistryEndpoint string
	// Applications supplies the experimental application directory profile.
	Applications         ApplicationsService
	ApplicationsEndpoint string
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
	router              *routerservice.Host
	routerIndex         int
	storage             *storageservice.Host
	storageIndex        int
	questions           *asksservice.Host
	questionIndex       int
	rights              *rightsservice.Host
	rightsIndex         int
	rightsOperatorIndex int
	rightsPolicy        *rights.DecisionPolicy
	credentials         CredentialsService
	credentialsIndex    int
	inference           InferenceService
	inferenceIndex      int
	providers           DeclaredProviders
	registry            RegistryService
	registryIndex       int
	applications        ApplicationsService
	applicationsIndex   int
	mu                  sync.Mutex
	candidates          []resolution.Candidate
	onError             func(error)
	startupErrors       []error
	started             atomic.Bool
}

// StartupErrors returns the provider startup failures Listen found, in order.
// They are the errors also passed to Options.OnError, and they are kept when
// OnError is nil. Each failed provider's contracts stay registered and resolve
// not_ready.
func (h *Host) StartupErrors() []error {
	return append([]error(nil), h.startupErrors...)
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
	if options.Router != nil && options.RouterEndpoint == "" {
		options.RouterEndpoint, err = bootstrap.Endpoint("router-v1")
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
	if err := validateCredentials(options); err != nil {
		return nil, err
	}
	if err := validateApplications(options); err != nil {
		return nil, err
	}
	if err := validateRegistry(options); err != nil {
		return nil, err
	}
	if err := validateInference(options); err != nil {
		return nil, err
	}
	if options.RightsPolicy == nil && (options.RightsEndpoint != "" || options.RightsEnforcer != nil || options.RightsOperator != nil || len(options.RightsActions) > 0) {
		return nil, errors.New("runtime: authorization requires an explicit decision policy")
	}
	if options.QuestionBook != nil && !options.QuestionBook.ApplicationProfile() || options.QuestionBook == nil && (options.QuestionEndpoint != "" || options.QuestionOperator != nil || options.QuestionAskPolicy != nil) {
		return nil, errors.New("runtime: questions require a separate application-profile book")
	}
	if (options.Storage != nil) != (options.StoragePolicy != nil) || (options.Storage == nil && options.StorageEndpoint != "") {
		return nil, errors.New("runtime: storage requires an explicit provider and content policy")
	}
	if options.StorageWritePolicy != nil || options.StorageWriteLimit != 0 || options.StorageWriteRecordPath != "" || options.StorageWriteRecords != nil {
		if _, writable := options.Storage.(storageservice.WritableStore); !writable || options.StorageWritePolicy == nil || options.StorageWriteLimit < 1 || !filepath.IsAbs(options.StorageWriteRecordPath) {
			return nil, errors.New("runtime: storage writes require a writable provider, explicit write policy, positive size limit and absolute record path")
		}
	}
	if (options.StorageChangesPolicy != nil || options.StorageChangesInterval != 0 || options.StorageChangesCapacity != 0) &&
		(options.Storage == nil || options.StorageChangesPolicy == nil || options.StorageChangesInterval < 0 || options.StorageChangesCapacity < 0) {
		return nil, errors.New("runtime: storage change observation requires a provider, explicit observe policy and nonnegative bounds")
	}
	if options.ModelPolicy != nil && options.ModelRegistry == nil {
		return nil, errors.New("runtime: model policy requires an explicit model registry")
	}
	if options.Router == nil && (options.RouterEndpoint != "" || options.RouterPolicy != nil) {
		return nil, errors.New("runtime: router configuration requires an explicit router provider")
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
	h := &Host{onError: options.OnError, modelIndex: -1, routerIndex: -1, storageIndex: -1, questionIndex: -1, rightsIndex: -1, rightsOperatorIndex: -1}
	var startupErrors []error
	var l *logservice.Host
	if options.Sink == nil {
		startupErrors = append(startupErrors, ErrNoLogSink)
	} else if l, err = logservice.Listen(options.LogEndpoint, options.Sink); err != nil {
		startupErrors = append(startupErrors, fmt.Errorf("runtime logging startup: %w", err))
	} else {
		h.logging = l
		l.OnError = options.OnError
		if options.LogHistoryPolicy != nil {
			if err := l.EnableHistoryPolicy(options.LogHistoryPolicy); err != nil {
				return nil, errors.Join(err, h.Close())
			}
		}
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
		if options.ConfigWithoutMachine {
			if err = c.OmitMachineRung(); err != nil {
				return nil, errors.Join(err, h.Close())
			}
		}
		if options.ConfigObservationSource != nil {
			if err = c.EnableObservation(options.ConfigObservationSource); err != nil {
				return nil, errors.Join(err, h.Close())
			}
		}
		if options.ConfigEditPolicy != nil {
			if err = c.EnableEditPolicy(options.ConfigEditPolicy); err != nil {
				return nil, errors.Join(err, h.Close())
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
			h.jobs.provider.SetErrorReporter(options.OnError)
		}
	}
	if options.ModelRegistry != nil {
		h.model, err = modelservice.Listen(options.ModelEndpoint, options.ModelRegistry)
		if err != nil {
			startupErrors = append(startupErrors, fmt.Errorf("runtime model startup: %w", err))
		} else {
			h.model.OnError = options.OnError
			if options.ModelPolicy != nil {
				if err := h.model.EnablePolicy(options.ModelPolicy); err != nil {
					return nil, errors.Join(err, h.Close())
				}
			}
			if options.ModelCredentials != nil {
				if err := h.model.EnableCredentials(options.ModelCredentials); err != nil {
					return nil, errors.Join(err, h.Close())
				}
			}
		}
	}
	if options.Router != nil {
		h.router, err = routerservice.Listen(options.RouterEndpoint, options.Router)
		if err != nil {
			startupErrors = append(startupErrors, fmt.Errorf("runtime router startup: %w", err))
		} else {
			h.router.OnError = options.OnError
			if options.RouterPolicy != nil {
				if err := h.router.EnablePolicy(options.RouterPolicy); err != nil {
					return nil, errors.Join(err, h.Close())
				}
			}
		}
	}
	if options.Storage != nil {
		h.storage, err = storageservice.Listen(options.StorageEndpoint, options.Storage, options.StoragePolicy)
		if err != nil {
			startupErrors = append(startupErrors, fmt.Errorf("runtime storage startup: %w", err))
		} else {
			h.storage.OnError = options.OnError
			if options.StorageWritePolicy != nil {
				records := options.StorageWriteRecords
				if records == nil {
					records = casapi.BoundedFileStore{MaxBytes: storageservice.MaxRecordFileBytes}
				}
				if err := h.storage.EnableWriter(options.StorageWritePolicy, options.StorageWriteLimit, records, options.StorageWriteRecordPath); err != nil {
					return nil, errors.Join(err, h.Close())
				}
			}
			if options.StorageChangesPolicy != nil {
				interval, capacity := options.StorageChangesInterval, options.StorageChangesCapacity
				if interval == 0 {
					interval = time.Second
				}
				if capacity == 0 {
					capacity = storageservice.DefaultChangeCapacity
				}
				if err := h.storage.EnableChanges(options.StorageChangesPolicy, interval, capacity); err != nil {
					return nil, errors.Join(err, h.Close())
				}
			}
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
					return nil, errors.Join(err, h.Close())
				}
			}
			if options.QuestionAskPolicy != nil {
				if err := h.questions.EnableAskPolicy(options.QuestionAskPolicy); err != nil {
					return nil, errors.Join(err, h.Close())
				}
			}
		}
	}
	if options.RightsPolicy != nil {
		err = registerRightsActions(options.RightsPolicy, options.RightsActions)
		if err == nil {
			h.rights, err = rightsservice.Listen(options.RightsEndpoint, options.RightsPolicy, options.RightsEnforcer)
		}
		if err != nil {
			startupErrors = append(startupErrors, fmt.Errorf("runtime rights startup: %w", err))
		} else {
			h.rights.OnError = options.OnError
			if options.RightsOperator != nil {
				if err := h.rights.EnableOperator(options.RightsOperator); err != nil {
					return nil, errors.Join(err, h.Close())
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
	// Without a logging provider the history contracts stay registered not_ready,
	// like the sink, so a client sees the missing sink instead of an unknown
	// contract. A provider that offers no history or observation omits them.
	if h.logging == nil || h.logging.HistoryAvailable() {
		history := h.candidates[0]
		history.Reference.Contract = "abstraction.logging/reader@1"
		h.candidates = append(h.candidates, history)
	}
	if h.logging == nil || h.logging.ObservationAvailable() {
		observer := h.candidates[0]
		observer.Reference.Contract = "abstraction.logging/observer@1"
		h.candidates = append(h.candidates, observer)
	}
	if h.jobs != nil {
		inventory := h.jobs.candidate(options.JobEndpoint)
		inventory.Reference.Contract = "abstraction.job/inventory@1"
		h.candidates = append(h.candidates, inventory)
		// The operator profile is served only when a method policy decides each
		// of its calls; without one it would forbid every call (JOB-A13).
		if options.JobMethodPolicy != nil {
			operator := h.jobs.candidate(options.JobEndpoint)
			operator.Reference.Contract = "abstraction.job/operator@1"
			h.candidates = append(h.candidates, operator)
		}
	}
	if options.ModelRegistry != nil {
		h.modelIndex = len(h.candidates)
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.model != nil, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.model", Contract: "abstraction.model/resolver@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.ModelEndpoint, Guarantees: []string{},
		}})
	}
	if options.Router != nil {
		h.routerIndex = len(h.candidates)
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.router != nil, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.router", Contract: "abstraction.router/router@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.RouterEndpoint, Guarantees: []string{},
		}})
	}
	if options.Storage != nil {
		h.storageIndex = len(h.candidates)
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.storage != nil, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.storage", Contract: "abstraction.storage/content-reader@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.StorageEndpoint, Guarantees: []string{},
		}})
	}
	if options.StorageChangesPolicy != nil {
		// Change observation shares the storage endpoint and its readiness lifetime.
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.storage != nil && h.storage.ChangesAvailable(), Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.storage", Contract: "abstraction.storage/content-changes@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.StorageEndpoint, Guarantees: []string{},
		}})
	}
	if options.StorageWritePolicy != nil {
		// The writer shares the storage endpoint and its readiness lifetime.
		h.candidates = append(h.candidates, resolution.Candidate{Ready: h.storage != nil && h.storage.WriterAvailable(), Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.storage", Contract: "abstraction.storage/content-writer@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.StorageEndpoint, Guarantees: []string{},
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
	// A decision point whose state cannot be read is not ready; Serve keeps
	// following the state (watchRightsState).
	rightsReady := h.rights != nil && options.RightsPolicy.CheckState(context.Background()) == nil
	if options.RightsPolicy != nil {
		h.rightsPolicy = options.RightsPolicy
		h.rightsIndex = len(h.candidates)
		h.candidates = append(h.candidates, resolution.Candidate{Ready: rightsReady, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.rights", Contract: "abstraction.rights/authorization@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.RightsEndpoint, Guarantees: []string{},
		}})
	}
	if options.RightsOperator != nil {
		h.rightsOperatorIndex = len(h.candidates)
		h.candidates = append(h.candidates, resolution.Candidate{Ready: rightsReady && h.rights.OperatorAvailable(), Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.rights", Contract: "abstraction.rights/operator@1", Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.RightsEndpoint, Guarantees: []string{},
		}})
	}
	h.addCredentialCandidates(options)
	h.addInferenceCandidate(options)
	h.addRegistryCandidate(options)
	h.addApplicationsCandidate(options)
	h.providers = options.Providers
	catalog, err := h.catalogue()
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
	if h.providers != nil {
		h.providers.Watch(h.refreshProviders)
	}
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
	if h.router != nil {
		h.router.OnStopped = func() { h.stopped(h.routerIndex, nil) }
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
	h.startupErrors = startupErrors
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
	if h.router != nil {
		errs = append(errs, h.router.Close())
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
	if h.credentials != nil {
		errs = append(errs, h.credentials.Close())
	}
	if h.inference != nil {
		errs = append(errs, h.inference.Close())
	}
	if h.providers != nil {
		errs = append(errs, h.providers.Close())
	}
	if h.applications != nil {
		errs = append(errs, h.applications.Close())
	}
	if h.registry != nil {
		errs = append(errs, h.registry.Close())
	}
	return errors.Join(errs...)
}

func (h *Host) stopped(index int, err error) {
	h.mu.Lock()
	// Contracts served by one endpoint share its readiness lifetime.
	endpoint := h.candidates[index].Reference.Endpoint
	for i := range h.candidates {
		if h.candidates[i].Reference.Endpoint == endpoint || index == h.inferenceIndex && h.candidates[i].Reference.Capability == "abstraction.inference" {
			h.candidates[i].Ready = false
		}
	}
	catalog, updateErr := h.catalogue()
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
	catalog, err := h.catalogue()
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
	if h.credentials != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(h.credentialsIndex, h.credentials.Serve(ctx)) }()
	}
	if h.inference != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(h.inferenceIndex, h.inference.Serve(ctx)) }()
	}
	if h.applications != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(h.applicationsIndex, h.applications.Serve(ctx)) }()
	}
	if h.registry != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(h.registryIndex, h.registry.Serve(ctx)) }()
	}
	if h.providers != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := h.providers.Serve(ctx); err != nil && h.onError != nil {
				h.onError(err)
			}
		}()
	}
	if h.rights != nil {
		workers.Add(2)
		go func() { defer workers.Done(); h.stopped(h.rightsIndex, h.rights.Serve(ctx)) }()
		go func() { defer workers.Done(); h.watchRightsState(ctx) }()
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
	if h.router != nil {
		workers.Add(1)
		go func() { defer workers.Done(); h.stopped(h.routerIndex, h.router.Serve(ctx)) }()
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
