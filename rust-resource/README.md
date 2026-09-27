# Resolved resource table and leases

Install status: source checkout only, version 0.0.0, not published to crates.io; no registry publish is planned for 0.3.0.

This optional crate adds `ResourceMachine` to the pure facade `Machine<C>`.
`resolve_resource_table(vec![], abstraction_facade_native::Scope::Local)`
returns `resource::TableClient<Binding<C>>` for `abstraction.resource/table@1`;
`resolve_resource_leases(vec![], abstraction_facade_native::Scope::Local)`
returns `resource::LeasesClient<Binding<C>>` for `abstraction.resource/leases@1`.
Select installed native IPC through the separate facade-native crate.

`TableClient::holders` reports who holds how much of a scarce resource, verified
by the instrument or claimed by the holder. `LeasesClient::acquire`, `renew`,
`release`, `observe` and `answer` ask for and yield resource on demand; a
predating holder is yielded on the service's behalf through the host's own
mechanism.

```rust
use abstraction_facade_resource::{ResourceMachine, resource};
use abstraction_facade_native::Scope;
let machine = abstraction_facade_native::discover();
let table = machine.resolve_resource_table(vec![], Scope::Local).expect("resource table");
let state = table.holders("card:0", false).expect("holders");
```

`acquire` returns the closed `AcquireOutcome` vocabulary: `acquired`,
`insufficient`, `holders_refused`, `not_permitted`, `unavailable` or `invalid`.
Call-level refusals are `internal`, `invalid_request`, `caller_refused`,
`unknown_resource`, `policy_unavailable` or `forbidden`.

The crate depends only on the facade core and the generated resource protocol,
which depends on the pure shared frame contract. The full resource and lease
vocabulary is in
[abstraction-resource's CONTRACT.md](https://github.com/openabstractions/abstraction-resource/blob/main/CONTRACT.md).
