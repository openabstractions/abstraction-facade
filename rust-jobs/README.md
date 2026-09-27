# Rust durable job bindings

Install status: source checkout only, version 0.0.0, not published to crates.io; no registry publish is planned for 0.3.0.

Add the separate `abstraction-facade-jobs` Cargo package and import `JobsMachine` to call `Machine::resolve_jobs`, `resolve_job_operations` or `resolve_job_inventory`. Resolution uses the pure base facade's validated `Binding<C>`. The package depends only on that core and generated job API; logging and native IPC sources are optional separate selections.

```rust
use abstraction_facade_jobs::{JobsMachine, jobs::wire};
use abstraction_facade_native::Scope;
let machine = abstraction_facade_native::discover();
let jobs = machine.resolve_jobs(vec![], Scope::Local).expect("jobs");
let history = jobs.history_window().expect("history");
let submission = wire::Submission { kind: "download".into(), ..Default::default() };
let accepted = jobs.submit(submission);
```

Jobs expose history_window, submit, reconcile, observe, cancel_work, read_result and copy_result. Save full request, key, history epoch, endpoint, owner and required guarantees (a guarantee is a contract-specific promise; see the [reference vocabulary](https://openabstractions.org/reference.html#vocabulary)) before submission. A transport failure leaves acceptance unknown; reconcile that same identity and retained binding. Jobs::restore(connector, endpoint, owner, required, deadline, cancellation) reconstructs it from caller-retained data, with no discovery. This is Rust's form of resuming work after restart; in Go it is `RestoreJobs`, in C++ it is client reconstruction, and in Python it is `Jobs.restore_installed()` / `Jobs.restore()`.

Resolved bindings preserve an explicit absolute deadline and cancellation signal. Default bindings provide a fresh five-second budget per call; copy_result shares one such budget across its chunks. with_waiting(deadline,cancellation) supplies a fresh caller budget while preserving endpoint and synchronized owner. Canceling a wait does not cancel accepted work. The separate Inventory binding has the same waiting accessor. Jobs::new also accepts an explicitly supplied generated transport; that caller controls its waiting policy. Composite copies additionally require the shared ScopedTransport contract.

copy_result streams one 64 KiB chunk at a time, pins operation ID/total for that copy, handles partial writes, and returns confirmed bytes. CopyError retains confirmed plus the typed original cause; non-data results become Error::Outcome without retries/polling. Blocking custom Write implementations remain the caller's responsibility. Lower-level read_result callers must preserve total/operation while assembling chunks themselves.

Inventory returns at most 64 snapshots per page, opaque continuation and explicit gap. Its negotiated read guarantees do not impose new promises on older receipts. The package owns no provider state, OS-specific I/O, or client persistence.
