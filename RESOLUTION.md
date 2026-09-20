# Service resolution

The additive `abstraction.facade/resolver@1` interface is defined in
[facade.thrift](facade.thrift). It selects a candidate binding; it does not
submit work, establish server trust or transfer ownership. The primary Go facade and C++ Machine accessors resolve registered services.
Explicit capability constructors accept a deliberately supplied endpoint.

An application requests a capability, one or more exact acceptable versioned
contract identities, required guarantees and permitted placement (`local`,
`remote` or `any`). Contract identities are exact strings, not semantic-version
ranges. Capability-specific accessors supply their supported contracts and
documented defaults. An empty guarantee list adds no requirement; an empty
contract list is invalid. Lists contain distinct nonempty names.

Registrations are runtime-owned, ordered by applicable policy and associated
with a readiness observation. Their logical provider identity is independent of
PID and endpoint. A registration has one concrete placement, transport binding,
endpoint and supported contract. It advertises candidate guarantees, not accepted
operation promises. No request field asserts caller identity or registration.

The receiving boundary supplies authorization for its bound caller. Missing
authorization denies access. Selection applies these stages in order:

| Stage | Failure status |
|---|---|
| Valid request shape and semantic inputs | `invalid_request` |
| Capability registered in an eligible placement | `unavailable` |
| At least one eligible candidate authorized | `forbidden` |
| At least one authorized candidate supports an acceptable contract | `incompatible` |
| At least one compatible candidate satisfies every required guarantee | `unmet_requirements` |
| At least one sufficient candidate ready | `not_ready` |

A registration may come from a provider declaration the person placed in the
runtime state; it follows the runtime's own registrations in policy order. A
sufficient registration that is not ready may be activated: the runtime starts
its on-demand provider, the request still reads `not_ready`, and a later
request finds it ready. Activation never waits inside a resolution.

Select the first ready sufficient candidate in policy order. Never combine
guarantees from different candidates, substitute an incompatible version, or
use a weaker candidate because a sufficient one is not ready. Contract-list
order is not a client preference that overrides runtime policy.

`resolved` carries exactly one reference. Every refusal carries no reference;
no unauthorized endpoint, provider name or guarantee list is returned. The
`forbidden` status may reveal that an eligible registration exists, but never
which one; deployments requiring catalogue existence privacy must restrict
access to this interface at the receiving boundary as well. A reference is
neither an authorization token nor proof that the endpoint remains available.
The service boundary must recheck authority, identity, compatibility and the
guarantees required when accepting work. A disconnected accepted operation is
reconciled against its owner, never sent back through fresh selection by this API.

## When resolution yields no service

An application that adopted a capability requires the service for it: when
resolution yields no service, the operation fails with the facade's resolution
error and the application substitutes nothing (VISION.md, "An adopted capability
with no runtime fails"). Every facade reports that failure with one error type
carrying a status, the requested capability and contract, what resolution looked
for, and a cause.

The status is the resolver's refusal word from the table above, or one of the
statuses a client reports without the resolver saying them:

| Client-side status | Meaning | Cause |
|---|---|---|
| `runtime_unavailable` | No runtime could be selected, or the selected or explicit endpoint could not be reached. | The selection or transport failure. |
| `invalid_resolution` | The resolver answered, and its answer failed validation: a refusal carrying a reference, or a reference that does not satisfy the request. | None, or the validation failure. |
| `unsupported_transport` | The resolver selected a reference whose scope or transport this binding cannot use. | None; Go, C++ and Rust name the reference's scope and transport. |
| `invalid_request` | The request was refused before it was sent. | None. |

`runtime_unavailable` is distinct from the resolver's `unavailable`: the first
means no runtime answered, the second that a reachable runtime serves no
registration for the capability. What resolution looked for is one of `the
installed runtime` (selection failed), `the installed runtime at <endpoint>` or
`the explicit endpoint <endpoint>`. A codec refusal, dispatch error or service
error from a resolver that did answer keeps its own type.

### Unsupported platforms

The runtime's platform declaration (`docs/platforms.json` in the source
repository, published in the layer catalogue) lists `android` (including Termux)
and `macos` as unsupported. On those platforms, resolving through the installed
runtime reports `runtime_unavailable` at `the installed runtime` before any
selection, with no cause, and the error's platform field names the platform:

`service resolution: runtime_unavailable: abstraction.logging/sink@1 (capability abstraction.logging) at the installed runtime: no supported OpenAbstractions runtime exists for android`

An explicit endpoint is the application's own choice and resolves as on any
other platform. Each binding detects the platform natively: Go from `GOOS`
during installed-runtime selection (`bootstrap.UnsupportedPlatformError`), C++
from `__ANDROID__` and `TARGET_OS_OSX` (or `ABSTRACTION_FACADE_TARGET_PLATFORM`),
Python from `sys.platform` and `sys.getandroidapilevel`, Rust from the
connector's `platform()` (default `std::env::consts::OS`), and JavaScript from
the connector's `platform` (default `process.platform`). On a declared platform
the Python `Machine()` loads no native library.

The caller's own cancellation stays the transport outcome, never a resolution
error. In Go, C++ and Rust a waiting budget already spent when resolution starts
also stays the transport outcome (the context error, `ipc::FrameError` with
`Status::Timeout`); Python and JavaScript report it as `runtime_unavailable`.

| Language | Error | Status | Context | Cause |
|---|---|---|---|---|
| Go | `*abstraction.ResolutionError` (`resolution.Error`), read with `errors.As` | `Status ResolutionErrorStatus`; constants `RuntimeUnavailable`, `InvalidResolution`, `UnsupportedTransport`; `Refusal()` returns the resolver's `ResolutionStatus` | `Capability`, `Contract`, `LookedFor`, `Scope`, `Transport`, `Platform` | `Unwrap()` (`errors.Is` reaches the selection or transport error) |
| C++ | `facade::ResolutionError : std::runtime_error` | `status`; `kRuntimeUnavailable`, `kInvalidResolution`, `kUnsupportedTransport`; `refusal()` returns the resolver's `ResolutionStatus` | `capability`, `contract`, `looked_for`, `scope`, `transport`, `platform` | `cause` (`std::exception_ptr`), `rethrow_cause()` |
| Rust | `Error::Resolution(ResolutionError<E>)` | `failure: ResolutionFailure` (`RuntimeUnavailable`, `Refused(ResolutionStatus)`, `InvalidResolution`, `UnsupportedTransport { scope, transport }`); `failure.status()` gives the word | `capability`, `contract`, `looked_for`, `platform: Option<&'static str>` | `cause: Option<E>`, `std::error::Error::source()` |
| Python | `ResolutionError` from `abstraction.facade.client` | `status`; `RUNTIME_UNAVAILABLE` | `capability`, `contract`, `looked_for`, `platform` | `__cause__` |
| JavaScript | `ResolutionError` from `@openabstractions/facade` | `status`; `runtimeUnavailable` | `capability`, `contract`, `lookedFor`, `platform` | `cause` |

The message reads the same everywhere:
`service resolution: runtime_unavailable: abstraction.logging/sink@1 (capability abstraction.logging) at the installed runtime`.
Go appends the cause's text, and Go, C++ and Rust append the unsupported
reference, as in `: remote reference over https`. Every binding appends
`: no supported OpenAbstractions runtime exists for <platform>` on a declared
unsupported platform, and Go then omits the cause's text.

### Reading a refusal

Go, C++ and Rust read the resolver's refusal through its typed accessor. Python
and JavaScript compare `status` with the generated `ResolutionStatus`
vocabulary. Each example tells a runtime that answered without the service from
a platform with no runtime.

Go (`client` is `github.com/openabstractions/abstraction-facade/go/client`,
`wire` is `.../go/abstraction/facade`):

```go
sink, err := client.Discover().ResolveLog(ctx, client.Requirements{})
var refused *client.ResolutionError
if errors.As(err, &refused) {
	if status, ok := refused.Refusal(); ok && status == wire.ResolutionStatusUnavailable {
		// A runtime answered and registers no logging sink.
	} else if refused.Status == client.RuntimeUnavailable && refused.Platform != "" {
		// No runtime exists for refused.Platform.
	}
}
```

C++:

```cpp
try {
    auto sink = abstraction::facade::Machine{}.resolve_log();
} catch (const abstraction::facade::ResolutionError& e) {
    if (e.refusal() == abstraction::facade::ResolutionStatus::Unavailable) {
        // A runtime answered and registers no logging sink.
    } else if (e.status == abstraction::facade::kRuntimeUnavailable && !e.platform.empty()) {
        // No runtime exists for e.platform.
    }
}
```

Rust (`abstraction_facade_native::discover()` and `abstraction_facade_service`):

```rust
use abstraction_facade_service::{wire::ResolutionStatus, Error, ResolutionFailure};
match abstraction_facade_native::discover().resolve_service("abstraction.logging/sink@1", vec![], "any") {
    Err(Error::Resolution(r)) => match (&r.failure, &r.platform) {
        (ResolutionFailure::Refused(ResolutionStatus::Unavailable), _) => { /* no logging sink registered */ }
        (ResolutionFailure::RuntimeUnavailable, Some(platform)) => { /* no runtime exists for platform */ }
        _ => {}
    },
    _ => {}
}
```

Python:

```python
import abstraction.facade as wire
from abstraction.facade.client import Machine, ResolutionError, RUNTIME_UNAVAILABLE

try:
    sink = Machine().resolve_log()
except ResolutionError as error:
    if error.status == wire.ResolutionStatus.UNAVAILABLE:
        pass  # A runtime answered and registers no logging sink.
    elif error.status == RUNTIME_UNAVAILABLE and error.platform is not None:
        pass  # No runtime exists for error.platform.
```

JavaScript:

```js
import {Machine, ResolutionError, runtimeUnavailable} from '@openabstractions/facade';
import {ResolutionStatus} from '@openabstractions/facade/protocol';

try {
  const sink = await new Machine(null, {connector}).resolveService('abstraction.logging/sink@1');
} catch (error) {
  if (!(error instanceof ResolutionError)) throw error;
  if (error.status === ResolutionStatus.Unavailable) { /* no logging sink registered */ }
  else if (error.status === runtimeUnavailable && error.platform !== null) { /* no runtime exists for error.platform */ }
}
```

The reference Go selector consumes immutable catalogue snapshots and per-caller
authorization. `HandleConnection` connects it to the existing identity-bound
framing, and the Go client carries its caller's context through the shared
transport. Client validation rejects a successful reference that does not satisfy
the request, or a refusal that carries a reference. These paths are exercised
over a real local test connection; they do not prove installed activation.
The implementation does not read a file registry, choose a remote authentication
protocol, install services or fall back to an embedded store. The
`testdata/resolution.json` corpus exercises the selection contract.

The Go `resolution.Host` now serves bounded concurrent connections and accepts
whole catalogue updates from its owning runtime. The Go `runtime` package
composes the existing logging and configuration service hosts, registers their
successfully opened endpoints and marks a stopped capability `not_ready` without
stopping its sibling. Its resolver permits only its service-account principal.
It is a user-runtime composition, not a privileged multi-user broker.

Supplying `runtime.Options.JobRoot` and `JobOwner` also enables recoverable job
admission through the existing job provider. `JobPolicy` can narrow which proven
same-owner programs may resolve and call it. Its caller scope survives process
restart, and both resolution and the resource endpoint enforce that policy.
Admission owns records for workers; enabling it does not start a download engine.
The central development command is `openabstractions serve runtime`, with
`--jobs-root` and `--jobs-owner` for this optional provider. Those are host settings;
applications receive typed service APIs and no storage path.

C++ `abstraction::facade_jobs` supplies independent `JobsClient`/`resolve_jobs`
without the logging/config/router APIs or embedded job store. It preserves
binding requirements into submissions and receipt validation, and pins the
logical owner learned from history or explicitly supplied for recovery. The
application retains its key, epoch and owner before sending; timeout never
becomes automatic resubmission. Windows tests run the same outside C++ program
before and after a runtime restart and prove a single retained operation.

Go `client.Machine.ResolveLog`, `ResolveConfig` and `ResolveRouter` resolve before
constructing the selected typed client. The corresponding C++ accessors use the
same reference binding. `oa-framed-local@1` binds the local OA endpoint for either
concrete execution scope, `local` or `remote`. The service enforces the selected
placement for every admitted call. Another transport is refused as
`unsupported_transport`. Older clients reject remote-scope native references;
their existing local bindings remain compatible. Bootstrap uses
the shared `runtime-v1` endpoint convention, or an explicitly supplied location.

The user runtime publishes built-in inference profiles twice when it has both
local and hosted routes. `openabstractions.user-runtime` is a `local` candidate
with `abstraction.inference/local-only@1`; its original inference endpoint now
enforces local execution. `openabstractions.user-runtime.remote` is a `remote`
candidate with `abstraction.inference/hosted-allowed@1` on a separate local IPC
endpoint. Applications using hosted inference must request `scope=remote` and a
binding version that accepts remote scope over `oa-framed-local@1`. Before this
split the Go user runtime advertised its mixed router as `scope=local`; callers
that used that inaccurate registration for hosted work must change the requested
scope. `scope=any` selects the first sufficient concrete candidate in runtime
policy order and retains that placement for the binding.
On Windows the default is
`\\.\pipe\openabstractions-user-<process-token SID>-runtime-v1`.
Runtime provider endpoints use the same account namespace. Go and C++ derive
the SID from the process token. An identity lookup failure is reported before
connection. Explicit endpoint overrides retain their supplied value.
Unix endpoint conventions and legacy standalone hosts retain their existing
names. Sessions belonging to one Windows account share this namespace;
installation must coordinate their runtime ownership. Endpoint naming supplies
location; server authentication remains a separate boundary.
Installation,
server trust, automatic activation and a waiting budget shared by subsequent
capability calls remain unfinished. These development APIs do not claim them.

Binding holds the selected endpoint. A later failure does not silently resolve
again or start a local provider. Applications can resolve for future independent
operations; uncertain accepted operations must reconcile against their owner.
The Windows runtime test uses non-default endpoints, invokes both capabilities,
checks unmet guarantees and absence, and proves sibling availability after one
host stops. It is an isolated runtime test, not an installer acceptance test.

From `go/`, run `go test ./resolution` in a configured source workspace. Generated
bindings and reference are reproduced from the repository's Thrift definition
with the published `abstractions/idl` generator using `go cpp python docs`.

For the independent C++ protocol, configure `cpp/` with
`-DABSTRACTION_FACADE_BUILD_AGGREGATE=OFF` and install it to a chosen prefix.
An outside consumer can then `find_package(abstraction_facade CONFIG REQUIRED)`
and link `abstraction::facade_protocol`, without logging/config/router packages.
`cpp/test/protocol` is such a consumer and checks generated typed calls and enum
refusal. The aggregate convenience client remains enabled by default for existing
consumers. A protocol target is not yet the installed runtime's resolver client.

## How the runtime bound its caller: abstraction.facade/caller@1

The resolver endpoint also serves `Caller.Observe`. It returns what the
receiving boundary bound for the connection's caller: the account (Windows SID
or decimal POSIX uid), the executable path and the pid, each at the Program
proof every runtime-hosted local service requires. Beside them it lists the
proof actually established for user, process, path, package and code, the
receiving platform's ceiling for each, and the transport, binding and stronger
transport of that ceiling. Observe grants, checks and changes nothing, and it
does not consult the resolution policy. A diagnostic client such as the
control panel uses it to show a person how the runtime sees that client.

Go: `client.Machine.ObserveCaller(ctx)` selects the runtime as resolution does,
under the same server trust, and returns the observation. A selection or
transport failure is the resolution error with capability `abstraction.facade`
and contract `abstraction.facade/caller@1`, and an observation whose identity
disagrees with its outcome is `invalid_resolution`. The server side is
`resolution.ObserveCaller(peer)`, dispatched by `HandleConnection` on the
service name. `go-core/resolution` `TestCallerEchoIsTheBoundProgramEvidence`
checks the echo against the test process's own account, executable, pid and
`identity.Ceiling()`. On Darwin the transport cannot meet Program proof and no
caller is echoed.

The contract rules, answered before generation:

1. **Outcomes.** `CallerOutcome` is the one outcome: `observed`; `unavailable`
   when the binding cannot be rechecked; `forbidden`, reserved for a boundary
   policy that withholds the echo; `invalid`, reserved for a request this
   provider cannot interpret. Observe addresses no record and writes nothing.
2. **Retained records.** None. The observation is computed per connection and
   never stored.
3. **Failures.** A refusal is its outcome word, and a transport failure is the
   client's resolution error. No failure cause is declared.
4. **Catalogues.** The attribute names are the five native `identity.Peer`
   attributes, and the proof words are `abstraction.identity` `Proof` names.
   An application extends neither.
5. **Identity fields.** The receiving runtime asserts every field. `mechanism`
   names the establishing facility, `identity/<os>`, as the logging service's
   stamp does. The request carries no identity. A caller the boundary cannot
   recheck receives `unavailable` with empty identity and pid -1.
6. **Bounds.** One reply fits a 1 MiB control frame: five attributes and one
   path.
7. **Unknown members.** `CallerOutcome` is acted on and refuses unknown
   members. The proof words are strings a reader displays.
8. **Entry points.** These rules judge the resolver endpoint's `caller@1`. A
   capability endpoint that later echoes its caller gets its own rule.
9. **Duplicate keys.** Refused, as for the whole definition.
10. **First definition.** Recorded as a "none" entry in
    `docs/BASE-PROTOCOL-CHANGES.md`.
11. **Reserved words.** No field, method or parameter name maps to a reserved
    word in Go, C++, Python, Rust or JavaScript.

The observation describes a local connection. A remote service states its own
authentication and never inherits these proofs.
