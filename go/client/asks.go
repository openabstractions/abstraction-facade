package client

import (
	"context"
	asks "github.com/openabstractions/abstraction-asks/go/client"
)

// ResolveAsks binds caller-owned question admission and observation. Human
// answers are supplied by the provider's independently authorized operator.
func (m *Machine) ResolveAsks(ctx context.Context, need Requirements) (*asks.Client, error) {
	endpoint, err := m.resolve(ctx, "abstraction.asks", AsksContract, need)
	if err != nil {
		return nil, err
	}
	return asks.NewWithTransport(endpoint), nil
}

// ResolveAsksChoices binds ordinary application choices with caller-retained
// pending/replay deadlines. Choosing an option grants no resource authority.
func (m *Machine) ResolveAsksChoices(ctx context.Context, need Requirements) (*asks.ChoiceClient, error) {
	endpoint, err := m.resolve(ctx, "abstraction.asks", AsksChoicesContract, need)
	if err != nil {
		return nil, err
	}
	return asks.NewChoiceWithTransport(endpoint), nil
}

// ResolveAsksNotifications binds this caller's notifications, whose actions
// the service holds after the caller exits (ASK-N1). Posting grants nothing.
func (m *Machine) ResolveAsksNotifications(ctx context.Context, need Requirements) (*asks.NotificationClient, error) {
	endpoint, err := m.resolve(ctx, "abstraction.asks", AsksNotificationContract, need)
	if err != nil {
		return nil, err
	}
	return asks.NewNotificationWithTransport(endpoint), nil
}

// ResolveAsksNotificationOperator binds the explicitly authorized presenter
// profile that lists, acts on and audits notifications (ASK-N4, ASK-N7).
func (m *Machine) ResolveAsksNotificationOperator(ctx context.Context, need Requirements) (*asks.NotificationOperator, error) {
	endpoint, err := m.resolve(ctx, "abstraction.asks", AsksNotificationOperatorContract, need)
	if err != nil {
		return nil, err
	}
	return asks.NewNotificationOperatorWithTransport(endpoint), nil
}

// ResolveAsksOperator binds the explicitly authorized human-answering profile.
// The receiving service checks operator authority on every method call.
func (m *Machine) ResolveAsksOperator(ctx context.Context, need Requirements) (*asks.Operator, error) {
	endpoint, err := m.resolve(ctx, "abstraction.asks", AsksOperatorContract, need)
	if err != nil {
		return nil, err
	}
	return asks.NewOperatorWithTransport(endpoint), nil
}

// ResolveAsksResponder binds the narrowly delegated first-use answer profile.
// The service checks exact option-specific rights grants on every call.
func (m *Machine) ResolveAsksResponder(ctx context.Context, need Requirements) (*asks.Responder, error) {
	endpoint, err := m.resolve(ctx, "abstraction.asks", AsksResponderContract, need)
	if err != nil {
		return nil, err
	}
	return asks.NewResponderWithTransport(endpoint), nil
}
