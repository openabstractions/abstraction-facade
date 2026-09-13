# Rust service client core

`abstraction-facade-service` provides transport-independent `Machine<C>`, validated
resolution and `Binding<C>`. Its only package dependency is the pure shared
`abstraction-frame` trait at `../../abstraction-identity/rust-frame`. Generated
resolver types remain in `../rs`. These are development source packages at 0.0.0.

Supply a `Connector` with `Machine::with_connector(endpoint, connector)`. The
connector implements the shared frame trait and enforces peer identity, supported
scope/transport, bounded frames, cancellation and the supplied absolute deadline.
Resolution validates the contract, provider, guarantees and scope before invoking
the selected endpoint. An alternate connector requires no native IPC link.

A resolved `Binding` retains the selected endpoint. An explicit absolute deadline
spans resolution and calls. Defaults provide five seconds per call. Shared
`ScopedTransport::call_scope` freezes one budget for a composite operation such
as a multi-chunk copy. `with_waiting` explicitly supplies a waiting policy on
the same binding; `with_limit` retains that policy and cancellation. `Binding::restore` uses a caller-retained endpoint
and supplied connector without discovery. Capability clients retain their own
logical ownership and receipt semantics.

Optional packages are independently selected:

- `../rust-native`: installed shared IPC connector and native bootstrap.
- `../rust-logging`: generated logging sink plus validated history helpers.
- `../rust-jobs`: acceptance, operation and inventory semantics.

Keep the sibling source paths declared by Cargo.toml and run
`cargo test --offline --manifest-path Cargo.toml`. Pure core and jobs tests require
no native library or logging package. Native consumers separately install the
identity C++ static library and set `OA_IPC_PREFIX` to its absolute prefix.
