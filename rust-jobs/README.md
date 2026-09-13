# Rust durable job bindings

Add the separate `abstraction-facade-jobs` Cargo package and import `JobsMachine` to call `Machine::resolve_jobs`, `resolve_job_operations` or `resolve_job_inventory`. Resolution uses the pure base facade's validated `Binding<C>`. The package depends only on that core and generated job API; logging and native IPC sources are optional separate selections. Source package versions remain 0.0.0; registry publication is not claimed.

Jobs expose history_window, submit, reconcile, observe, cancel_work, read_result and copy_result. Save full request, key, history epoch, endpoint, owner and required guarantees before submission. A transport failure leaves acceptance unknown; reconcile that same identity and retained binding. Jobs::restore(connector, endpoint, owner, required, deadline, cancellation) reconstructs it from caller-retained data, with no discovery.

Resolved bindings preserve an explicit absolute deadline and cancellation signal. Default bindings provide a fresh five-second budget per call; copy_result shares one such budget across its chunks. with_waiting(deadline,cancellation) supplies a fresh caller budget while preserving endpoint and synchronized owner. Canceling a wait does not cancel accepted work. The separate Inventory binding has the same waiting accessor. Jobs::new also accepts an explicitly supplied generated transport; that caller controls its waiting policy. Composite copies additionally require the shared ScopedTransport contract.

copy_result streams one 64 KiB chunk at a time, pins operation ID/total for that copy, handles partial writes, and returns confirmed bytes. CopyError retains confirmed plus the typed original cause; non-data results become Error::Outcome without retries/polling. Blocking custom Write implementations remain the caller's responsibility. Lower-level read_result callers must preserve total/operation while assembling chunks themselves.

Inventory returns at most 64 snapshots per page, opaque continuation and explicit gap. Its negotiated read guarantees do not impose new promises on older receipts. The package owns no provider state, OS-specific I/O, or client persistence.
