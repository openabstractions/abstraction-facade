# abstraction-facade-model

Install status: source checkout only, version 0.0.0, not published to crates.io; no registry publish is planned for 0.3.0.

Resolves `abstraction.model/resolver@1` through a facade `Machine` and wraps the
generated client. `resolve` returns the typed lookup result after checking its
contract: only `resolved` carries a portable download request, with a SHA256
digest and anonymous HTTP(S) sources. `forbidden` is an evaluated lookup
refusal and `unavailable` means the decision or registry could not answer. The
router crate's analogous refusal is named `policy_unavailable`, not `unavailable`.

```rust
use abstraction_facade_model::{ModelMachine, wire};
use abstraction_facade_native::Scope;
let machine = abstraction_facade_native::discover();
let model = machine.resolve_model(vec![], Scope::Local).expect("model");
let result = model.resolve(wire::Ref { repo: "local/chat".into(), ..Default::default() });
```

The generated `abstraction-model-api` crate imports its request records from
`abstraction-download-request-api` through `--rust-import`, and this crate
re-exports both. Select `rust-native` separately for native transport.
