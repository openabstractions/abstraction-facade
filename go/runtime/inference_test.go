package runtime

import (
	"context"
	"testing"

	wire "github.com/openabstractions/abstraction-facade/go/abstraction/facade"
)

type discoveryInference struct{ transcription, speech, image bool }

func (d *discoveryInference) Serve(context.Context) error  { return nil }
func (d *discoveryInference) Close() error                 { return nil }
func (d *discoveryInference) TranscriptionAvailable() bool { return d.transcription }
func (d *discoveryInference) SpeechAvailable() bool        { return d.speech }
func (d *discoveryInference) ImageAvailable() bool         { return d.image }

func TestTranscriptionDiscoveryRequiresReadyProvider(t *testing.T) {
	for _, ready := range []bool{false, true} {
		h := &Host{}
		h.addInferenceCandidate(Options{Inference: &discoveryInference{transcription: ready}, InferenceEndpoint: "inference-endpoint"})
		found := false
		for _, candidate := range h.candidates {
			if candidate.Reference.Contract == InferenceTranscriptionContract {
				found = true
			}
		}
		if found != ready {
			t.Fatalf("ready %v, transcription candidate %v", ready, found)
		}
	}
}

func TestInferencePlacementCandidatesUseDistinctProvidersAndEndpoints(t *testing.T) {
	h := &Host{}
	h.addInferenceCandidate(Options{Inference: &discoveryInference{}, InferenceEndpoint: "inference-local", InferenceRemoteEndpoint: "inference-remote"})
	if len(h.candidates) != 2 {
		t.Fatalf("candidates %+v", h.candidates)
	}
	local, remote := h.candidates[0].Reference, h.candidates[1].Reference
	if local.Scope != wire.ScopeLocal || local.Endpoint != "inference-local" || len(local.Guarantees) != 1 || local.Guarantees[0] != InferenceLocalGuarantee {
		t.Fatalf("local candidate %+v", local)
	}
	if remote.Scope != wire.ScopeRemote || remote.Endpoint != "inference-remote" || remote.Provider == local.Provider || len(remote.Guarantees) != 1 || remote.Guarantees[0] != InferenceHostedGuarantee {
		t.Fatalf("remote candidate %+v", remote)
	}
}

func TestSpeechDiscoveryRequiresReadyProvider(t *testing.T) {
	for _, ready := range []bool{false, true} {
		h := &Host{}
		h.addInferenceCandidate(Options{Inference: &discoveryInference{speech: ready}, InferenceEndpoint: "inference-endpoint"})
		found := false
		for _, candidate := range h.candidates {
			if candidate.Reference.Contract == InferenceSpeechContract {
				found = true
			}
		}
		if found != ready {
			t.Fatalf("ready %v, speech candidate %v", ready, found)
		}
	}
}

func TestImageDiscoveryRequiresReadyProvider(t *testing.T) {
	for _, ready := range []bool{false, true} {
		h := &Host{}
		h.addInferenceCandidate(Options{Inference: &discoveryInference{image: ready}, InferenceEndpoint: "inference-endpoint"})
		found := false
		for _, candidate := range h.candidates {
			if candidate.Reference.Contract == InferenceImageContract {
				found = true
			}
		}
		if found != ready {
			t.Fatalf("ready %v, image candidate %v", ready, found)
		}
	}
}
