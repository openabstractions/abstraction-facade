# Resolved questions and question operator

Install status: source checkout only, version 0.0.0, not published to crates.io; no registry publish is planned for 0.3.0.

This optional crate adds `AsksMachine` to the pure facade `Machine<C>`.
`resolve_asks(vec![], abstraction_facade_native::Scope::Local)` returns `Questions<Binding<C>>` for
`abstraction.asks/application@1`; `resolve_asks_operator(vec![], abstraction_facade_native::Scope::Local)` returns
`Operator<Binding<C>>` for `abstraction.asks/operator@1`. Select installed native
IPC through the separate facade-native crate.

`Questions::ask` and `observe` admit and observe questions in the caller's bound
scope. `gone` reports a retired or forgotten question and is neither approval
nor refusal. `Operator::list`, `answer` and `retire` are each subject to the
host's operator policy; `forbidden` and `unavailable` carry no records. Retiring
returns the question's record on the retiring call and none when replayed.
Inputs are validated before any exchange, and results are checked against their
outcome shapes. No call is retried automatically.

```rust
use abstraction_facade_asks::{AsksMachine, wire};
use abstraction_facade_native::Scope;
let machine = abstraction_facade_native::discover();
let questions = machine.resolve_asks(vec![], Scope::Local).expect("asks");
let observation = questions.ask(wire::ApplicationQuestion {
    request_key: "req-1".into(), key: "confirm-delete".into(), ..Default::default()
}).expect("ask");
```

The crate depends only on the facade core and the generated asks protocol, which
depends on the pure shared frame contract.
