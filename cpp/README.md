# C++ resolution bindings

`abstraction::facade_client` supplies explicit service resolution alongside the
legacy conventional-endpoint accessors. `abstraction::facade_protocol` remains
independently installable with `ABSTRACTION_FACADE_BUILD_AGGREGATE=OFF` and has no
capability or transport dependencies.

```cpp
#include <abstraction/facade/client.hpp>

abstraction::facade::Machine machine;
auto logger = machine.ResolveLog();
logger.Log(1, "connected through resolution");
auto config = machine.ResolveConfig().Read();
```

`ResolveLog`, `ResolveConfig` and `ResolveRouter` accept required guarantee names
and a scope (`any` by default). Each operation asks the runtime for the exact
supported capability contract, validates the result and creates the existing
capability client at the selected endpoint. A client already returned keeps that
endpoint; it does not silently reselect or replay work when an operation fails.
Legacy `Log`, `Config` and `Router` remain unchanged.

`Machine(explicit_runtime_endpoint)` and `ResolutionClient(endpoint, timeout_ms)`
allow explicit bootstrap, including isolated tests. The default bootstrap uses
`ABSTRACTION_RUNTIME_ENDPOINT` when nonempty, otherwise the established
local pipe/socket convention. On Windows the process-token SID supplies
`\\.\pipe\openabstractions-user-<canonical SID>-runtime-v1`.
SID lookup errors propagate. Unix uses the existing runtime-v1 socket convention.
The resolver uses the existing
identity IPC `FrameTransport`; this does not prove server authentication.

`ResolutionClient::Resolve` returns the validated `ResolveResult`, including
refusal statuses. Typed accessors throw `ResolutionError` with the refusal in
`.status`. Inconsistent or weaker references throw `invalid_resolution`.
Only concrete `local` references with transport `oa-framed-local@1` can be bound;
other transports and remote placement throw `unsupported_transport`. Generated
codec errors and IPC transport failures propagate. No fallback, store, activation
or production listener is supplied here.

`cpp/test/binding` is an outside CMake package consumer: configure it with only the
installed prefix in `CMAKE_PREFIX_PATH`, build, then run CTest. It checks semantic
refusals and forged references; on Windows it additionally exercises real named
pipes for bootstrap and the selected logging/config/router endpoint using private
single-frame test fixtures. No provider endpoint environment override is used.

Both consumers expose `--default-runtime-endpoint` for comparison with the Go
bootstrap, including an explicit environment override. Windows CTest also checks
identity lookup failures and token cleanup through injected API failures.

The consumer's `--runtime <endpoint>` mode uses an actual external runtime and
starts no listeners. It resolves logging, writes `cpp-resolved-log`, then resolves
and reads config. Runtime integration tests can provide this executable through
`OA_CPP_RESOLUTION_PROBE` and verify their retained sink received that event.


## Job acceptance without unrelated capabilities

Configure with `ABSTRACTION_FACADE_BUILD_AGGREGATE=OFF` and
`ABSTRACTION_FACADE_BUILD_JOBS=ON` to install `abstraction::facade_jobs` alone.
Its dependencies are the generated `abstraction::job_acceptance` protocol and
shared `abstraction::ipc`. It brings no embedded job store, CAS/watch, or
logging/config/router dependency. `abstraction_job_acceptance` can also be
installed directly from job/cpp with `ABSTRACTION_JOB_BUILD_LEGACY=OFF`.

```cpp
#include <abstraction/facade/jobs.hpp>
using namespace abstraction::facade;
auto jobs = ResolveJobs(ResolutionClient(explicit_runtime_endpoint),
                        {"abstraction.job/reconciliation@1"});
auto history = jobs.GetHistoryWindow();
// Persist the caller-owned key, history epoch and logical owner before sending.
job_api::Submission submission;
submission.identity.key = persisted_caller_key;
submission.identity.history_epoch = history.history_epoch;
submission.kind = "download";
submission.spec = {'{', '}'};
submission.required_guarantees = {"abstraction.job/reconciliation@1"};
auto result = jobs.Submit(submission);
// After an ambiguous outcome, reconcile this identity at this same binding.
auto recovered = jobs.Reconcile(submission.identity);
```

The aggregate `Machine::ResolveJobs` forwards to that same binder. `JobsClient`
implements the generated `RecoverableAcceptance` interface and owns a fixed
shared transport with a 2 MiB frame limit matching the provider. Identities and
cancellation remain explicit. Neither transport failure nor `unknown` triggers
another submission or provider selection. Returned receipts are checked against
request identity and requested guarantees; callers retain and verify their
expected logical owner across restart. Admission is not downstream completion.

Both installed consumers accept `--runtime <endpoint> --jobs <caller-key>`.
The job-only consumer under `cpp/test/jobs` makes history, Submit and Reconcile
calls without logging/config calls. It prints the reconciled generated
AcceptanceResult JSON, including logical owner, operation ID, key and epoch,
only after both receipts agree. The original logging/config probe mode remains
available in `cpp/test/binding`.
