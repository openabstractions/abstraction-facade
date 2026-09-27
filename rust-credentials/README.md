# Resolved credentials holder and applier

Install status: source checkout only, version 0.0.0, not published to crates.io; no registry publish is planned for 0.3.0.

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

```rust
use abstraction_facade_credentials::{CredentialsMachine, wire};
use abstraction_facade_native::Scope;
let machine = abstraction_facade_native::discover();
let credentials = machine.resolve_credentials(vec![], Scope::Local).expect("credentials");
let result = credentials.store("", wire::Registration {
    name: "openrouter".into(), kind: "header".into(), header: "Authorization".into(),
    secret: b"...".to_vec(), ..Default::default()
});
```

The crate depends only on the facade core and the generated credentials
protocol, which depends on the pure shared frame contract.
