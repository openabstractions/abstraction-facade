# Native Rust facade connector

This optional development crate supplies `NativeConnector`, the concrete `Machine`
alias, and `discover()`. It uses `abstraction-ipc` and the installed shared C ABI.
Install the identity C++ static library and set `OA_IPC_PREFIX` to its absolute
prefix before Cargo builds. The connector supports local `oa-framed-local@1` and
preserves the shared binding deadline, cancellation signal and frame limit.
