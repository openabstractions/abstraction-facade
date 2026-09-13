# C++ resolution bindings

`abstraction::facade_client` supplies service resolution through every Machine
capability accessor. `abstraction::facade_protocol` remains
independently installable with `ABSTRACTION_FACADE_BUILD_AGGREGATE=OFF` and has no
capability or transport dependencies.

```cpp
#include <abstraction/facade/client.hpp>

abstraction::facade::Machine machine;
auto logger = machine.Log();
logger.Log(1, "connected through resolution");
auto config = machine.Config().Read();
```

`ResolveLog`, `ResolveConfig` and `ResolveRouter` accept required guarantee names
and a scope (`any` by default). Each accessor asks the runtime for the exact
supported capability contract, validates the result and creates the existing
capability client at the selected endpoint. A client already returned keeps that
endpoint; it does not silently reselect or replay work when an operation fails.
`Log`, `Config` and `Router` delegate to these resolvers with no required
guarantees and scope `any`. They now require an available runtime and can throw
typed resolution refusals during accessor evaluation. Applications deliberately
supplying a provider endpoint can construct `logging::Logger(endpoint)`,
`config::Client(endpoint)` or `router::Client(endpoint)` directly.

An explicit deadline gives resolution and the selected logging/config/router operation
one waiting budget:

```cpp
const auto deadline = abstraction::ipc::Clock::now() + std::chrono::seconds(2);
auto logger = machine.ResolveLog({}, "any", deadline);
logger.Log(1, "connected within the caller budget");
// Other typed calls use the same overload:
// machine.ResolveConfig({}, "any", deadline).Read();
// machine.ResolveRouter({}, "any", deadline).Hosts();
```

The deadline uses the existing monotonic IPC clock. The resolver, connection,
frame write and reply share it. These explicit bindings and their copies retain
the deadline, including time spent by the caller between resolution and use;
use a new explicit scope for later work. Default `Resolve*` is one SDK call;
a method on its returned reusable client is a later SDK call. Resolution has a
five-second default budget. Each subsequent logging/config method has two
seconds, router method ten seconds, and job method five seconds. Chaining
`machine.ResolveConfig().Read()` retains these separate budgets. Applications
that treat resolution and use as one end-to-end operation pass an absolute
deadline as above. Repeated calls on a default client receive fresh per-call
budgets and keep the selected endpoint.

Expired deadlines throw `ipc::FrameError` with `Status::timeout` before opening a
connection. In-flight timeouts retain that type. Stopping the wait supplies no
cancellation of provider work already sent; `Machine.WithCancellation(token)` propagates the shared IPC cancellation
token through resolution and the returned capability client. Direct `logging::Logger`, `config::Client`, `ResolutionClient::Resolve`,
`router::Client`, `JobsClient`, and shared `FrameTransport` also accept the same absolute deadline.

`Machine(explicit_runtime_endpoint)` and `ResolutionClient(endpoint, timeout_ms)`
allow explicit bootstrap, including isolated tests. The default bootstrap uses
`ABSTRACTION_RUNTIME_ENDPOINT` when nonempty, otherwise the established
local pipe/socket convention. On Windows the process-token SID supplies
`\\.\pipe\openabstractions-user-<canonical SID>-runtime-v1`.
SID lookup errors propagate. Unix uses the existing runtime-v1 socket convention.
Default `Machine` and `ResolutionClient` select independent installed-runtime
identity on their first call through the shared native selector. Selection uses
the same deadline and cancellation token as resolution. The owned account/program
expectation follows resolution into every selected local provider connection;
untrusted or unavailable native proof produces a typed `ipc::FrameError`.

Copies retain the first installation snapshot, including reusable clients with
fresh call budgets. Construct a new machine to select a changed installation.
Explicit endpoint constructors accept independently supplied
`WithServerExpectation(ipc::ServerExpectation{kind, principal, program})` evidence.
An explicit endpoint without an expectation retains the explicitly configured
transport behavior. Endpoint environment overrides do not supply identity.
Native selection supports the platforms implemented by the installed IPC library;
unsupported selection returns `Status::proof_unavailable`.

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


## Build and install

For a fixed, publicly obtainable revision set, use the [resolved logging example](examples/logging/README.md). Its lock file and commands require no revision selection. The instructions below describe development source builds.

Requirements: CMake 3.16 or newer, a C++17 compiler and the platform SDK. Windows
uses the native Windows SDK; Linux and macOS use their native compiler and SDK.
These commands build libraries and an outside consumer. They install no OS service.

Place reviewed public source checkouts beside one another:
`abstraction-facade`, `abstraction-identity`, `abstraction-job`,
`abstraction-logging`, `abstraction-config`, and `abstraction-router`.
Each is available at `https://github.com/openabstractions/<name>`.
Record `git rev-parse HEAD` for all six. Select revisions containing the APIs in
this README and use that same set for deployment; untagged development changes
may need a coordinated release before those revisions are publicly obtainable.
CMake uses installed dependencies or these sibling checkouts and fetches nothing.

From their common parent, set `PREFIX` to an absolute writable path and run
these commands in Bash (including Git Bash on Windows):

```sh
PREFIX="C:/work/logging-example/prefix" # edit once; on Unix use an absolute Unix path
cmake -S abstraction-facade/cpp -B build/facade -DCMAKE_BUILD_TYPE=Release -DABSTRACTION_FACADE_BUILD_AGGREGATE=ON -DCMAKE_INSTALL_PREFIX="$PREFIX"
cmake --build build/facade --config Release
cmake --install build/facade --config Release
```

The installed prefix contains the facade and its transitive client/protocol
packages. Put the README's `main.cpp` and `CMakeLists.txt` in a separate `my-app`
directory, then build using only that prefix:

```sh
cmake -S my-app -B build/my-app -DCMAKE_BUILD_TYPE=Release -DCMAKE_PREFIX_PATH="$PREFIX"
cmake --build build/my-app --config Release
```

A compatible runtime must be running before executing the application. An absent
resolver fails the accessor. It leaves provider installation and activation with
the operator. The repository's `.github/workflows/facade-binding.yml` records the
same installed-package approach and the exact dependency revisions for each run.

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

For a single budget spanning job resolution and admission, use
`ResolveJobs(resolver, guarantees, scope, deadline)` or
`machine.ResolveJobs(guarantees, scope, deadline)`. History lookup and submission
on that client consume the same deadline, including caller time between calls.
Persist identity and logical owner before submission. An expired wait leaves
acceptance unresolved; it can occur after the provider accepted the request.

A fresh reconciliation budget preserves the binding and its synchronized owner
pin. This helper uses the existing typed API and never resubmits after timeout:

```cpp
abstraction::facade::job_api::AcceptanceResult submit_or_reconcile(
    const abstraction::facade::JobsClient& jobs,
    const abstraction::facade::job_api::Submission& submission) {
    using namespace std::chrono_literals;
    auto attempt = jobs.WithDeadline(abstraction::ipc::Clock::now() + 5s);
    try {
        return attempt.Submit(submission);
    } catch (const abstraction::ipc::FrameError& error) {
        if (error.status != abstraction::ipc::Status::timeout) throw;
        auto recovery = jobs.WithDeadline(abstraction::ipc::Clock::now() + 5s);
        return recovery.Reconcile(submission.identity);
    }
}
```

`WithDeadline` leaves the original client unchanged and shares its exact endpoint,
required guarantees and logical-owner pin. Reconciliation may itself time out or
return an unresolved outcome; callers preserve the same identity for subsequent
recovery. Waiting expiry does not call `CancelWork`. After a caller restart,
restore the persisted endpoint and logical owner through the existing JobsClient
constructor instead of resolving an existing operation to another provider.

The same binding offers `ObserveWork`, `ReadResult` and `CopyResult`. Observation
returns typed state, progress, cancellation intent and last-attempt failure class.
`CancelWork` acknowledges intent; subsequent observation reports whether
cancellation or completion won. The service supplies complete result bytes in
chunks of at most 64 KiB. `CopyResult` checks identity and total across chunks
and writes to an application-owned `std::ostream`:

```cpp
auto observation = jobs.ObserveWork(submission.identity);
if (observation.outcome == "observed" &&
    observation.snapshot->state == "complete") {
    auto copied = jobs.CopyResult(submission.identity, destination);
    if (copied.error) std::rethrow_exception(copied.error);
}
```

Here `destination` is the application's output stream. Partial output remains
with that stream on error. `confirmed` counts confirmed writes; a throwing
stream buffer can leave the current write's partial count unknown. A fresh
`WithDeadline` supplies a budget for later observation or copying.
`ResolveJobOperations` selects the operation contract for new bindings; retain
the original binding for accepted work. Admission-only providers explicitly
return `unsupported` for result reads.

Both installed consumers accept `--runtime <endpoint> --jobs <caller-key>`.
The job-only consumer under `cpp/test/jobs` makes history, Submit and Reconcile
calls without logging/config calls. It prints the reconciled generated
AcceptanceResult JSON, including logical owner, operation ID, key and epoch,
only after both receipts agree. The original logging/config probe mode remains
available in `cpp/test/binding`.

## User configuration edits

`Machine.ResolveConfigEditor()` resolves `abstraction.config/editor@1` and returns
`config::Editor`. The existing `Config()` accessor retains read semantics.

```cpp
auto editor = machine.ResolveConfigEditor();
auto snapshot = editor.ReadUser();
auto values = snapshot.values;
values.off["example"] = "disabled";
auto update = editor.ReplaceUser(snapshot.revision, values);
// update.outcome is applied or conflict; update.snapshot carries current values.
```

Replacement sends only the user rung and its expected revision. Conflict is
returned to the caller without retry. The service owns persistence. A deliberate
provider endpoint can be supplied to `config::Editor(endpoint)`. Copies made with
`WithDeadline` and `WithCancellation` preserve the other waiting constraint.
The resolver's deadline overload shares one budget with the returned editor.
Cancellation ends waiting and can leave a sent replacement's outcome unknown;
read the current snapshot before deciding on another edit.

## Runtime observations

`Machine.Observe()` returns a `RuntimeObservationResult` containing the shared
`RuntimeObservation` vocabulary and an optional `std::exception_ptr error`.
Default requests cover logging, config reader, job acceptance, job operations
and config editor with local scope and no additional guarantees. All resolution
calls share one five-second deadline; the explicit overload accepts the caller's
absolute deadline. `WithCancellation` applies to the whole observation.

```cpp
auto result = machine.Observe();
for (const auto& capability : result.observation.capabilities) {
    if (capability.result) {
        // Inspect the original resolver status and optional selected reference.
    }
}
if (result.error) {
    // Earlier answers remain available; later requests are unobserved.
}
```

Bootstrap defaults to unknown. Supply `BootstrapObservation` only from independent
installation/supervisor evidence; this accessor never probes the OS or upgrades
bootstrap state when the resolver answers. A running supervisor can coexist with
not_ready or forbidden capabilities. Missing transport produces an error and
absent capability results, preserving any bootstrap evidence supplied by the
caller. It does not establish installation state. Resolution is an observation
of eligibility; clients establish provider authorization on the actual call.

## Optional storage binding

Build with `ABSTRACTION_FACADE_BUILD_AGGREGATE=OFF` and
`ABSTRACTION_FACADE_BUILD_STORAGE=ON`. The installed
`abstraction_facade_storage` package exports `abstraction::facade_storage` and
requires only `abstraction_facade_resolution`, `abstraction_storage_content`,
and shared IPC. Aggregate builds do not enable storage automatically.
`ABSTRACTION_FACADE_BUILD_RESOLUTION=ON` builds the resolver independently.

```cmake
find_package(abstraction_facade_storage CONFIG REQUIRED)
target_link_libraries(app PRIVATE abstraction::facade_storage)
```

```cpp
#include <abstraction/facade/storage.hpp>
auto deadline = abstraction::ipc::Clock::now() + std::chrono::seconds(5);
abstraction::facade::ResolutionClient resolver;
auto content = abstraction::facade::ResolveStorage(resolver, {}, "local", deadline);
// Open a canonical sha256 digest, then Read the returned Resource in bounded chunks.
```

Storage resolution and all uses of this returned client share one deadline and
cancellation token. Omitting the deadline starts a fresh five-second scope at
resolution; resolve again for a later operation. The selected endpoint remains
fixed. A resource is an unverified naming lookup: hash assembled bytes yourself.
Content access requires the receiving service's explicit policy. Refusal, gap,
or changed content cannot be interpreted as successful end of content.

The resolver's separate package is `abstraction_facade_resolution`, target
`abstraction::facade_resolution`. Its export contains the generated protocol and
shared IPC binding, so a resolver consumer does not load capability packages.
The existing `abstraction_facade` package still supplies configured jobs and
aggregate targets.

## Optional question and rights bindings

Enable `ABSTRACTION_FACADE_BUILD_ASKS` and/or `ABSTRACTION_FACADE_BUILD_RIGHTS`
with aggregate disabled for independent adoption. Installed packages
`abstraction_facade_asks` / `abstraction_facade_rights` export corresponding
`abstraction::facade_asks` / `abstraction::facade_rights` targets. They depend on
the independent resolver and their capability package, plus shared IPC.

Headers `abstraction/facade/asks.hpp` and `abstraction/facade/rights.hpp` provide
ResolveAsks and ResolveRights. Each accepts a ResolutionClient, required
guarantees and scope, with an optional absolute deadline. Resolution and future
calls retain that same deadline and cancellation token; the default starts a
fresh five-second operation scope at resolution. A new operation needs a new
scope. The selected endpoint remains fixed.

ResolveTrustedRightsEnforcer exposes the relay operation separately. Its caller
must be a receiver-designated enforcement point supplying actual bound subject
evidence. A question answer grants no resource access itself. These bindings
provide no operator answering method or live awake lease.

## Logging observation

`ABSTRACTION_FACADE_BUILD_LOGGING=ON` with aggregate disabled installs
`abstraction_facade_logging`, target `abstraction::facade_logging`. It requires
only the logging capability package and independent resolver/shared IPC.
`abstraction/facade/logging.hpp` supplies ResolveLog, ResolveLogReader, and
ResolveLogObserver. Aggregate Machine also exposes ResolveLogObserver.

`logging::Observer.Observe(cursor, max_records, max_bytes, wait_ms)` long-polls
one selected history instance. The default call budget is two seconds;
wait_ms (0..30000) cannot extend it. For longer waits, use the accessor overload
accepting an absolute IPC deadline. That same deadline and cancellation token
apply through resolution and the returned binding. Default bindings have fresh
per-call budgets; explicit bindings retain their absolute deadline.

Pages use the existing history record/byte limits and continuation rules.
At-end reports a current observation; later logs may arrive. Gap requires an
explicit new traversal. Cancellation ends this wait and retains the logs; it
does not acknowledge persistence or cancel a writer. A provider must implement
notification-backed observation to advertise this contract. Unsupported
providers retain their existing sink/history capabilities.

## Generated typed service binding

The independent `abstraction::facade_resolution` target supports generated C++
service descriptors. Install the requested capability's protocol package alongside
it. For configuration, link `abstraction::config_client` and use:

```cpp
#include <abstraction/facade/resolution.hpp>
#include <abstraction/config/rec.h>
auto observer = abstraction::facade::ResolveService<
    abstraction::config::ConfigObserverService>(abstraction::facade::ResolutionClient{});
auto observation = observer->Observe({}, "", 0);
```

Every generated service descriptor supplies its authoritative wire name, derived
capability and typed Client alias. ConfigReader and other generated services use
the same factory. The returned owning binding can move safely; the client reached
through operator-> is borrowed for that binding's lifetime. Default bindings use
fresh per-call timeouts. The overload accepting an absolute IPC deadline preserves
that deadline through resolution and calls. Resolver cancellation is retained.

`BindService<Descriptor>(reference, transport, guarantees, scope)` accepts an
explicit alternative transport and validates the same reference requirements.
That transport owns its identity, scope, waiting and framing guarantees. Resolution
uses shared local IPC and refuses unsupported transports. Neither path chooses a
local provider implementation or retries uncertain calls. Generated response
codecs retain their declared validation; domain-specific semantic helpers remain
separate where needed. These descriptors are current-source functionality.
