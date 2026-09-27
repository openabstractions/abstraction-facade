# abstraction-facade-router

Install status: source checkout only, version 0.0.0, not published to crates.io; no registry publish is planned for 0.3.0.

Resolves `abstraction.router/router@1` through a facade `Machine` and wraps the
generated client. `models` and `hosts` read inventory; `pick` chooses a
permitted host and refuses an empty model before any call. The router's policy
decides every call. `Error::service_code` returns `forbidden` for an evaluated
denial and `policy_unavailable` when the decision could not be obtained. The
model crate's analogous refusal is named `unavailable`, not `policy_unavailable`.

```rust
use abstraction_facade_router::{RouterMachine, wire};
use abstraction_facade_native::Scope;
let machine = abstraction_facade_native::discover();
let router = machine.resolve_router(vec![], Scope::Local).expect("router");
let hosts = router.hosts(false).expect("hosts");
let picked = router.pick(wire::PickRequest { model: "local/chat".into(), ..Default::default() });
```

The crate depends on the pure resolver core and the generated
`abstraction-router-api` crate. Select `rust-native` separately for native
transport.
