# Resolved rights decisions and policy administration

Install status: source checkout only, version 0.0.0, not published to crates.io; no registry publish is planned for 0.3.0.

This optional crate adds `RightsMachine` to the pure facade `Machine<C>`.
`resolve_rights(vec![], abstraction_facade_native::Scope::Local)` returns
`Decisions<Binding<C>>` for `abstraction.rights/authorization@1`;
`resolve_rights_operator(vec![], abstraction_facade_native::Scope::Local)` returns
`Operator<Binding<C>>` for `abstraction.rights/operator@1`. Select installed
native IPC through the separate facade-native crate.

`Decisions::decide` and `decide_for` are point-in-time observations on the
caller's own or an asserted subject. `Operator::list_policy`, `set_rule` and
`revoke_rule` compare an expected policy revision; a lost reply is uncertain
and is never retried. Inputs are validated before any exchange, and results
are checked against their outcome shapes.

```rust
use abstraction_facade_rights::{RightsMachine, wire};
use abstraction_facade_native::Scope;
let machine = abstraction_facade_native::discover();
let rights = machine.resolve_rights(vec![], Scope::Local).expect("rights");
let decision = rights.decide("fixture.read", "r").expect("decide");
```

`decide`/`decide_for` return `permitted`, `denied`, `not_granted` or
`unknown_action` with a policy revision, or `invalid`, `forbidden` or
`unavailable` with none. `set_rule`/`revoke_rule` return `applied` or
`conflict` with a revision and the edited target's current rule, or the same
three unevaluated refusals with neither.

The crate depends only on the facade core and the generated rights protocol,
which depends on the pure shared frame contract. The full decision and policy
vocabulary is in
[abstraction-rights's CONTRACT.md](https://github.com/openabstractions/abstraction-rights/blob/main/CONTRACT.md).
