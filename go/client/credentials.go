package client

import (
	"context"

	credentials "github.com/openabstractions/abstraction-credentials/go/client"
)

// ResolveCredentials binds the credential holder of the caller's account:
// register, rotate, revoke, list and audit credentials by name. Every call is a
// rights decision on the bound caller, and no call returns secret bytes.
func (m *Machine) ResolveCredentials(ctx context.Context, need Requirements) (*credentials.Holder, error) {
	endpoint, err := m.resolve(ctx, "abstraction.credentials", CredentialsContract, need)
	if err != nil {
		return nil, err
	}
	return credentials.NewHolderWithTransport(endpoint), nil
}

// ResolveCredentialsApplier binds the applier for a consuming service the
// receiving host designated as an enforcer. Any other program reads forbidden.
func (m *Machine) ResolveCredentialsApplier(ctx context.Context, need Requirements) (*credentials.Applier, error) {
	endpoint, err := m.resolve(ctx, "abstraction.credentials", CredentialsApplierContract, need)
	if err != nil {
		return nil, err
	}
	return credentials.NewApplierWithTransport(endpoint), nil
}

// ResolveCredentialReader explicitly selects raw saved-value disclosure to the
// bound requester. The returned value belongs to the app and must be cleared
// after use. Legacy Use/manage grants confer no reader authority.
func (m *Machine) ResolveCredentialReader(ctx context.Context, need Requirements) (*credentials.SecretReader, error) {
	endpoint, err := m.resolve(ctx, "abstraction.credentials", CredentialsReaderContract, need)
	if err != nil {
		return nil, err
	}
	return credentials.NewSecretReaderWithTransport(endpoint), nil
}

// ResolveScopedCredentials selects name-scoped saved-secret management. It
// provides named Metadata and conditional edits; account enumeration is separate.
func (m *Machine) ResolveScopedCredentials(ctx context.Context, need Requirements) (*credentials.ScopedHolder, error) {
	endpoint, err := m.resolve(ctx, "abstraction.credentials", CredentialsScopedHolderContract, need)
	if err != nil {
		return nil, err
	}
	return credentials.NewScopedHolderWithTransport(endpoint), nil
}

// ResolveScopedCredentialsApplier selects the typed Use profile explicitly.
// Calls require a designated consuming-service enforcer and never fall back.
func (m *Machine) ResolveScopedCredentialsApplier(ctx context.Context, need Requirements) (*credentials.ScopedApplier, error) {
	endpoint, err := m.resolve(ctx, "abstraction.credentials", CredentialsScopedApplierContract, need)
	if err != nil {
		return nil, err
	}
	return credentials.NewScopedApplierWithTransport(endpoint), nil
}
