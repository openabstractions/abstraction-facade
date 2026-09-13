# Native Rust facade connector

The native default example uses endpoint-only compatibility selection. Configure
independent server expectations for verified local use. It does not establish
the installed-discovery trust guarantees of the Go/C++/Python defaults.


This optional development crate supplies `NativeConnector`, the concrete `Machine`
alias, and `discover()`. It uses `abstraction-ipc` and the installed shared C ABI.
Install the identity C++ static library and set `OA_IPC_PREFIX` to its absolute
prefix before Cargo builds. The connector supports local `oa-framed-local@1` and
preserves the shared binding deadline, cancellation signal and frame limit.
