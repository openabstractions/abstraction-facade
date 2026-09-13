# Rust logging facade

The native default example uses endpoint-only compatibility selection. Configure
independent server expectations for verified local use. It does not establish
the installed-discovery trust guarantees of the Go/C++/Python defaults.


This optional development crate depends on the pure facade core and generated
logging API. It has no native dependency. Import `LoggingMachine` for resolved
sink and history access. Choose the separate native connector for installed IPC:

```rust
use abstraction_facade_logging::{LoggingMachine, logging::{Record, Sink}};
let machine = abstraction_facade_native::discover().expect("bootstrap");
let log = machine.resolve_log(vec![], "local").expect("logging");
log.Write(Record { schema: 1, time: "2026-09-13T00:00:00Z".into(),
    msg: "service client".into(), ..Default::default() }).expect("submission");
```

Sink completion confirms local submission. Waiting errors preserve uncertain
outcomes and never trigger retries. Default bindings receive a fresh budget per call. Explicit absolute deadlines
remain fixed; with_waiting replaces waiting policy while retaining the binding.
History validates bounds, continuation and explicit gap/refusal outcomes.
