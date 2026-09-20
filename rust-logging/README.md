# Rust logging facade

This optional development crate depends on the pure facade core and generated
logging API. It has no native dependency. Import `LoggingMachine` for resolved
sink and history access. Choose the separate native connector for installed IPC:

The native example uses `rust-native::discover()`. Each resolution selects the
installed runtime's identity through the shared C ABI selector and requires that
identity on both resolver and provider connections.

```rust
use abstraction_facade_logging::{LoggingMachine, logging::{Record, Sink}};
use abstraction_facade_native::Scope;
let machine = abstraction_facade_native::discover();
let log = machine.resolve_log(vec![], Scope::Local).expect("logging");
log.write(Record { schema: 1, time: "2026-09-13T00:00:00Z".into(),
    msg: "service client".into(), ..Default::default() }).expect("submission");
```

Sink completion confirms local submission. A direct `SinkClient` write never
retries, and its waiting errors preserve uncertain outcomes.

`AsyncWriter` over `ResolvedSink` is the handler an application keeps
(CONTRACT.md `LOG-S12`, `LOG-S13`). `write` and `log` queue and return at once.
The writer retries a failed delivery with backoff, rebinds through the machine,
drops the newest record when its 1024-record queue is full, and delivers a gap
record on recovery. `counts()` reports the loss, and with no callbacks each
transition is one line on standard error:

```rust
use abstraction_facade_logging::{AsyncOptions, AsyncWriter, ResolvedSink};
use abstraction_facade_native::Scope;
let sink = ResolvedSink::resolve(abstraction_facade_native::discover(), vec![], Scope::Local).expect("logging");
let log = AsyncWriter::new(sink, AsyncOptions { program: "app".into(), ..Default::default() });
log.log(0, "service client", Default::default());
```

Default bindings receive a fresh budget per call. Explicit absolute deadlines
remain fixed; with_waiting replaces waiting policy while retaining the binding.
History validates bounds, continuation and explicit gap/refusal outcomes.
