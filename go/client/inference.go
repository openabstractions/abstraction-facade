package client

import (
	"context"

	inference "github.com/openabstractions/abstraction-inference/go/client"
)

// ResolveInference binds the model call service of the selected runtime:
// Complete and Stream a request by model family. The runtime picks the host,
// decides abstraction.inference/complete for the bound caller and applies a
// named credential itself; no key or endpoint reaches the application.
func (m *Machine) ResolveInference(ctx context.Context, need Requirements) (*inference.Chat, error) {
	endpoint, err := m.resolve(ctx, "abstraction.inference", InferenceContract, need)
	if err != nil {
		return nil, err
	}
	return inference.NewWithTransport(endpoint), nil
}

// ResolveInferenceChatV2 binds the versioned explicit model or named policy
// service. A runtime without chat@2 returns a resolution error.
func (m *Machine) ResolveInferenceChatV2(ctx context.Context, need Requirements) (*inference.ChatV2, error) {
	endpoint, err := m.resolve(ctx, "abstraction.inference", InferenceChatV2Contract, need)
	if err != nil {
		return nil, err
	}
	return inference.NewChatV2WithTransport(endpoint), nil
}

// ResolveInferenceSelectionOperatorV2 binds the scoped policy editor with
// optional logical provider pins.
func (m *Machine) ResolveInferenceSelectionOperatorV2(ctx context.Context, need Requirements) (*inference.SelectionOperatorV2, error) {
	endpoint, err := m.resolve(ctx, "abstraction.inference", InferenceSelectionOperatorV2Contract, need)
	if err != nil {
		return nil, err
	}
	return inference.NewSelectionOperatorV2WithTransport(endpoint), nil
}

// ResolveInferenceImageV2 binds image generation by explicit model or policy.
func (m *Machine) ResolveInferenceImageV2(ctx context.Context, need Requirements) (*inference.ImageV2, error) {
	endpoint, err := m.resolve(ctx, "abstraction.inference", InferenceImageV2Contract, need)
	if err != nil {
		return nil, err
	}
	return inference.NewImageV2WithTransport(endpoint), nil
}

// ResolveEmbeddings binds the embeddings service of the selected runtime:
// Embed texts by model family. The runtime picks a host serving embeddings,
// decides abstraction.inference/complete for the bound caller and applies a
// named credential itself; no key or endpoint reaches the application.
func (m *Machine) ResolveEmbeddings(ctx context.Context, need Requirements) (*inference.Embeddings, error) {
	endpoint, err := m.resolve(ctx, "abstraction.inference", EmbeddingsContract, need)
	if err != nil {
		return nil, err
	}
	return inference.NewEmbeddingsWithTransport(endpoint), nil
}

// ResolveInferenceOperator binds the runtime's inference operator profile:
// hosts, the gateway window's local keys and the inference audit. The runtime
// decides host.manage, key.issue or audit.read for this program on each call.
func (m *Machine) ResolveInferenceOperator(ctx context.Context, need Requirements) (*inference.Operator, error) {
	endpoint, err := m.resolve(ctx, "abstraction.inference", InferenceOperatorContract, need)
	if err != nil {
		return nil, err
	}
	return inference.NewOperatorWithTransport(endpoint), nil
}
