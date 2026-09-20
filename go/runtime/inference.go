package runtime

import (
	"context"
	"errors"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
	"github.com/openabstractions/abstraction-facade/go/resolution"
	inferencewire "github.com/openabstractions/abstraction-inference/go/abstraction/inference/api"
)

// InferenceContract is the wire contract an inference service publishes.
const InferenceContract = "abstraction.inference/chat@1"

const (
	InferenceLocalGuarantee  = "abstraction.inference/local-only@1"
	InferenceHostedGuarantee = "abstraction.inference/hosted-allowed@1"
)

// InferenceOperatorContract is the operator profile an inference service
// publishes beside chat@1 when it reports OperatorAvailable.
const InferenceOperatorContract = "abstraction.inference/operator@1"

// InferenceEmbedContract is the embeddings profile an inference service
// publishes beside chat@1 when it reports EmbedAvailable.
const InferenceEmbedContract = "abstraction.inference/embed@1"

// InferenceTranscriptionContract is read from the generated dispatcher so
// discovery cannot drift from the service's schema contract.
var InferenceTranscriptionContract = new(inferencewire.TranscriptionDispatcher).ServiceContract()

// InferenceSpeechContract is read from the generated dispatcher.
var InferenceSpeechContract = new(inferencewire.SpeechDispatcher).ServiceContract()

// InferenceImageContract is read from the generated dispatcher.
var InferenceImageContract = new(inferencewire.ImageDispatcher).ServiceContract()

// InferenceService is a separately composed inference host, for example
// abstraction-inference/go/service.Host with its provider, already listening
// on Options.InferenceEndpoint. The runtime serves it, publishes
// abstraction.inference/chat@1 and closes it with the runtime. Its router,
// decision function, credentials applier and ceilings belong to the
// composition; its rights actions go in Options.RightsActions.
type InferenceService interface {
	Serve(context.Context) error
	Close() error
}

func validateInference(options Options) error {
	if (options.Inference == nil) != (options.InferenceEndpoint == "") {
		return errors.New("runtime: inference requires an explicit service and its endpoint")
	}
	if options.Inference == nil && options.InferenceRemoteEndpoint != "" {
		return errors.New("runtime: remote inference endpoint requires an inference service")
	}
	return nil
}

func (h *Host) addInferenceCandidate(options Options) {
	h.inferenceIndex = -1
	if options.Inference == nil {
		return
	}
	h.inference = options.Inference
	h.inferenceIndex = len(h.candidates)
	add := func(contract string) {
		h.candidates = append(h.candidates, resolution.Candidate{Ready: true, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.inference", Contract: contract,
			Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.InferenceEndpoint, Guarantees: []string{InferenceLocalGuarantee},
		}})
		if options.InferenceRemoteEndpoint != "" {
			h.candidates = append(h.candidates, resolution.Candidate{Ready: true, Reference: wire.ServiceReference{
				Provider: "openabstractions.user-runtime.remote", Capability: "abstraction.inference", Contract: contract,
				Scope: wire.ScopeRemote, Transport: resolution.LocalTransport, Endpoint: options.InferenceRemoteEndpoint, Guarantees: []string{InferenceHostedGuarantee},
			}})
		}
	}
	add(InferenceContract)
	// A service that also serves embeddings on its endpoint says so.
	if embed, ok := options.Inference.(interface{ EmbedAvailable() bool }); ok && embed.EmbedAvailable() {
		add(InferenceEmbedContract)
	}
	// A service that has a ready transcription provider on this endpoint says so.
	if transcription, ok := options.Inference.(interface{ TranscriptionAvailable() bool }); ok && transcription.TranscriptionAvailable() {
		add(InferenceTranscriptionContract)
	}
	if speech, ok := options.Inference.(interface{ SpeechAvailable() bool }); ok && speech.SpeechAvailable() {
		add(InferenceSpeechContract)
	}
	if images, ok := options.Inference.(interface{ ImageAvailable() bool }); ok && images.ImageAvailable() {
		add(InferenceImageContract)
	}
	if live, ok := options.Inference.(interface{ LiveAvailable() bool }); ok && live.LiveAvailable() {
		add(new(inferencewire.LiveDispatcher).ServiceContract())
	}
	// A service that also serves the operator profile on its endpoint says so.
	if operator, ok := options.Inference.(interface{ OperatorAvailable() bool }); ok && operator.OperatorAvailable() {
		h.candidates = append(h.candidates, resolution.Candidate{Ready: true, Reference: wire.ServiceReference{
			Provider: "openabstractions.user-runtime", Capability: "abstraction.inference", Contract: InferenceOperatorContract,
			Scope: wire.ScopeLocal, Transport: resolution.LocalTransport, Endpoint: options.InferenceEndpoint, Guarantees: []string{},
		}})
	}
}
