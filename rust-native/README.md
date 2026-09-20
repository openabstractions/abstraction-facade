# Native Rust facade connector

This optional development crate supplies `NativeConnector`, the concrete `Machine`
alias, and `discover()`.

`discover()` resolves through the installed runtime. Each resolution selects the
runtime's identity with the shared C ABI selector (`oa_ipc_select_runtime`), the
one the Go, C++ and Python defaults use, then reads the runtime's endpoint. The
resolver connection and the selected provider connection both require that
identity. No installation, an ambiguous one, an unreachable runtime or a server
that fails the identity check is `Error::Resolution` with
`ResolutionFailure::RuntimeUnavailable`. The endpoint override variable moves the
endpoint and never supplies trust.

`Machine::new(endpoint)` keeps explicit endpoints unverified for compatibility.
`Machine::with_connector(endpoint, NativeConnector::verified(server))` requires an
independently obtained `ServerExpectation` on every connection.

It uses `abstraction-ipc` and the installed shared C ABI. Install the identity C++
static library and set `OA_IPC_PREFIX` to its absolute prefix before Cargo builds.
`OA_IPC_CHECK_ONLY=1 cargo check` type-checks without that prefix and links
nothing. The connector supports local `oa-framed-local@1` and preserves the shared
binding deadline, cancellation signal and frame limit.
