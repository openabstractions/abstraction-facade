# Rust logging facade

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
outcomes and never trigger retries. Bindings retain one absolute waiting budget;
resolve again or explicitly reconstruct waiting for a new application operation.
History validates bounds, continuation and explicit gap/refusal outcomes.
