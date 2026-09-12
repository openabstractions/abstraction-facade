# Service resolution

The additive `abstraction.facade/resolver@1` interface is defined in
[facade.thrift](facade.thrift). It selects a candidate binding; it does not
submit work, establish server trust or transfer ownership. The existing facade
accessors remain legacy conventional-endpoint clients until explicitly migrated.

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

C++ `abstraction::facade_jobs` supplies independent `JobsClient`/`ResolveJobs`
without the logging/config/router APIs or embedded job store. It preserves
binding requirements into submissions and receipt validation, and pins the
logical owner learned from history or explicitly supplied for recovery. The
application retains its key, epoch and owner before sending; timeout never
becomes automatic resubmission. Windows tests run the same outside C++ program
before and after a runtime restart and prove a single retained operation.

Go `client.Machine.ResolveLog`, `ResolveConfig` and `ResolveRouter` resolve before
constructing the selected typed client. The corresponding C++ accessors use the
same reference binding. This first binding supports `oa-framed-local@1` with
`local` scope only; another transport or remote reference is refused as
`unsupported_transport`, never interpreted as a local endpoint. Bootstrap uses
the shared `runtime-v1` endpoint convention, or an explicitly supplied location.
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
