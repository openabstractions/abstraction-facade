# Resolved credentials holder and applier

This optional crate adds `CredentialsMachine` to the pure facade `Machine<C>`.
`resolve_credentials(vec![], abstraction_facade_native::Scope::Local)` returns `Credentials<Binding<C>>` for
`abstraction.credentials/holder@1`; `resolve_credentials_applier(vec![], abstraction_facade_native::Scope::Local)`
returns `CredentialApplier<Binding<C>>` for `abstraction.credentials/applier@1`.
Select installed native IPC through the separate facade-native crate.

`Credentials::store`, `rotate`, `revoke`, `list` and `audit` are each a rights
decision on the bound caller, and no reply carries secret bytes. The applier
serves only programs the receiving host designated as enforcers; every other
caller reads `forbidden`. Inputs are validated before any exchange, results are
checked against their outcome shapes, and no call is retried.

The crate depends only on the facade core and the generated credentials
protocol, which depends on the pure shared frame contract. Source version 0.0.0
is current development packaging, not a registry release.
