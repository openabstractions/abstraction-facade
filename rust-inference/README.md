# Resolved model calls

This optional crate adds `InferenceMachine` to the pure facade `Machine<C>`.
`resolve_inference(vec![], abstraction_facade_native::Scope::Local)` returns `Chat<C>` for
`abstraction.inference/chat@1`. Select installed native IPC through the separate
facade-native crate.

`Chat::complete` starts a request, observes it to its end and folds the deltas
into the reply. `Chat::stream` returns an `Iterator` of
`Result<Delta, Error>`; dropping it before the end delta cancels the operation.
A start refusal is a reply, or one end delta, carrying that outcome. Each
observe gets a fresh budget of five seconds plus its wait. Pages are checked
against their outcome shapes, and start is never retried.

The crate depends only on the facade core and the generated inference protocol.
Source version 0.0.0 is current development packaging, not a registry release.
