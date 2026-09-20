# abstraction-facade

Ask this computer to keep work running, call a model, read shared content, write
an event, ask a person, or find a supported application that is open. The facade
finds an allowed service that can do the job and returns the matching client.
The service keeps its data, credentials and execution state.

The same application code can use a built-in service or an operator's existing
downloader, model engine or remote provider. The application states the behavior
it requires; selection and failures stay explicit. **Development API**: this page
describes the source at this revision; release qualification is tracked
separately.

## Start here

Use the aggregate facade when an application needs one or more OA services.
Choose a narrow language package when dependency size matters:

| Language | Starting point | Native installed-runtime connector |
| --- | --- | --- |
| Go | `facade.Discover()` | Included |
| C++17 | `abstraction::facade::Machine` | `abstraction::facade_client` |
| Python | `Machine()` | Shared native library |
| Rust | `abstraction_facade_native::Machine` plus a capability trait | `rust-native` |
| JavaScript | `new Machine(null, {connector})` plus a generated client | `@openabstractions/ipc` |

Give every resolution and call a finite budget. Ask for the exact contract,
placement and guarantees the operation needs. Keep a durable job's binding and
request identity before submitting it. A resolution error carries the stage and
typed status needed to present an unavailable, forbidden or incompatible
service accurately.

Operators can add an existing provider with `openabstractions provider add`,
inspect it with `provider list`, and remove it from future selection with
`provider remove`. Work already accepted continues with the same provider.
Applications can find or open visible programs through `openabstractions
applications`; the [application directory
section](#application-directory-and-activation) describes the client and its
safety checks.

## Resolution

The additive `abstraction.facade/resolver@1` interface is declared in
[facade.thrift](facade.thrift) and its selection and refusal semantics are in
[RESOLUTION.md](RESOLUTION.md). An application names a capability, one or more
exact acceptable contract identities, required guarantees and a placement
(`local`, `remote` or `any`); the resolver selects the first ready, authorized,
compatible candidate in policy order, or fails at the first unsatisfied stage
(`unavailable`, `forbidden`, `incompatible`, `unmet_requirements`, `not_ready`).
A resolved reference is a candidate binding, not proof of a successful call;
the service boundary rechecks authority on every use.

`Discover()` (Go), `Machine()` (Python), `Machine machine;` (C++),
`abstraction_facade_native::discover()` (Rust) and `new Machine(null, {connector})`
(JavaScript) resolve through the installed runtime. Each `Resolve*`/`resolve_*` accessor checks the
returned capability, exact contract, requested guarantees, scope, endpoint and
transport before creating the typed client.

## Go

Requirements: Go 1.26 or newer and a running compatible runtime.

```go
package main

import (
    "context"
    "log"
    "time"

    facade "github.com/openabstractions/abstraction-facade/go"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    events, err := facade.Discover().ResolveLog(ctx, facade.Requirements{})
    if err != nil {
        log.Fatal(err)
    }
    if err := events.LogContext(ctx, 0, "worker started", map[string]string{"component": "worker"}); err != nil {
        log.Fatal(err)
    }
}
```

`facade.Discover()` returns `*Machine` (an alias for `client.Machine`).
`client.Machine` in `github.com/openabstractions/abstraction-facade/go/client`
exposes: `ResolveLog`, `ResolveLogReader`, `ResolveLogObserver`, `ResolveConfig`,
`ResolveConfigEditor`, `ResolveConfigObserver`, `ResolveRouter`, `ResolveModel`,
`ResolveJobs`, `ResolveJobOperations`, `ResolveJobInventory`, `RestoreJobs`,
`ResolveStorage`, `ResolveStorageWriter`, `ResolveStorageChanges`,
`ResolveAsks`, `ResolveAsksOperator`, `ResolveRights`, `ResolveRightsOperator`
`ResolveApplications`, and `Observe` (a point-in-time diagnostic scan over
several requests at once).
`RestoreJobs` rebinds a saved `JobsBinding` after a restart without resolving
again. `NewVerified`/`WithProviderTrust` bind an explicit endpoint and
independent server trust for a deliberately supplied provider.

For a released revision, create an application module and select that exact
facade revision:

```sh
go mod init example.com/my-app
go get github.com/openabstractions/abstraction-facade/go@<reviewed-revision>
go build .
```

For coordinated development checkouts, use a Go workspace containing the
public modules; this tests source compatibility, not release availability.
Job recovery, request identity and the scope a submission is filed under are
covered in [go/client/README.md](go/client/README.md).

## C++17

```cmake
cmake_minimum_required(VERSION 3.16)
project(my_app LANGUAGES CXX)
find_package(abstraction_facade CONFIG REQUIRED)
add_executable(my_app main.cpp)
target_link_libraries(my_app PRIVATE abstraction::facade_client)
```

```cpp
#include <abstraction/facade/client.hpp>
#include <iostream>

int main() {
    try {
        abstraction::facade::Machine machine;
        auto events = machine.log();
        events.log(0, "worker started", {{"component", "worker"}});
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
```

`Machine::log()`, `config()` and `router()` are convenience accessors over
`resolve_log`, `resolve_config` and `resolve_router` with no required
guarantees and scope `any`; each `resolve_*` form takes explicit guarantees, a
scope and an optional absolute deadline. The aggregate client also exposes
`resolve_config_editor`, `resolve_jobs`/`resolve_job_operations` and
`observe()`. Optional packages (`ABSTRACTION_FACADE_BUILD_AGGREGATE=OFF` plus a
`BUILD_*` flag) install narrower targets: `abstraction::facade_jobs`,
`abstraction::facade_storage`, `abstraction::facade_asks`,
`abstraction::facade_rights`, `abstraction::facade_logging` and the dependency-free
`abstraction::facade_resolution`/`abstraction::facade_protocol`. Full build,
install and job-recovery detail: [cpp/README.md](cpp/README.md).

## Python

```python
import time
from abstraction.facade.client import Machine
from abstraction.facade import Scope

machine = Machine(deadline=time.monotonic() + 2)
sink = machine.resolve_log(scope=Scope.LOCAL)
sink.write(...)
```

`Machine` selects the installed runtime's identity through the shared native
library on first use. Its accessors: `resolve_storage`,
`resolve_storage_writer`, `resolve_storage_changes`, `resolve_log`,
`resolve_log_reader`, `resolve_log_observer`, `resolve_config`,
`resolve_config_editor`, `resolve_config_observer`, `resolve_asks`,
`resolve_asks_operator`, `resolve_model`, `resolve_router`, `resolve_jobs`,
`resolve_job_operations`, `resolve_job_inventory`, `resolve_rights` and
`resolve_rights_operator`, plus `resolve_applications` for the application
directory. Install: [py/README.md](py/README.md).

## Rust

`Machine<C>` in the `abstraction-facade-service` crate (`rust/`) is
transport-independent: it needs a `Connector` and offers only the generic
`resolve_service`. Each capability is an independently selected crate adding an
extension trait:

| crate | trait | adds |
| --- | --- | --- |
| `rust-config` | `ConfigMachine` | `resolve_config`, `resolve_config_editor`, `resolve_config_observer` |
| `rust-logging` | `LoggingMachine` | `resolve_log`, `resolve_log_reader`, `resolve_log_observer` |
| `rust-jobs` | `JobsMachine` | `resolve_jobs`, `resolve_job_operations`, `resolve_job_inventory` |
| `rust-storage` | `StorageMachine` | `resolve_storage`, `resolve_storage_writer`, `resolve_storage_changes` |
| `rust-asks` | `AsksMachine` | `resolve_asks`, `resolve_asks_operator` |
| `rust-rights` | `RightsMachine` | `resolve_rights`, `resolve_rights_operator` |
| `rust-router` | `RouterMachine` | `resolve_router`, then `models`, `hosts`, `pick` on the returned client |
| `rust-model` | `ModelMachine` | `resolve_model`, then `resolve` on the returned client, over `abstraction.model/resolver@1` |

`rust-native` supplies `NativeConnector`, the installed shared IPC connector;
without it, supply another `Connector` implementation. `discover()` selects the
installed runtime's identity through the shared C ABI selector, as the Go, C++
and Python defaults do, and requires it on the resolver and provider connections. Package detail:
[rust/README.md](rust/README.md), [rust-config/README.md](rust-config/README.md).

## JavaScript

```js
import {Machine} from '@openabstractions/facade';
import {NativeConnector} from '@openabstractions/ipc';
import {SinkClient} from '@openabstractions/logging';
const connector = new NativeConnector();
const machine = new Machine(null, {connector});
const binding = await machine.resolveService('abstraction.logging/sink@1');
const log = binding.client(SinkClient);
```

The JavaScript package has one generic `resolveService(contract)` method, not
a named accessor per capability; a connector supplies `supports(scope,
transport)` and `connect`. `new Machine(null, {connector})` selects the
installed runtime through `connector.runtimeEndpoint()` at each resolution.
Like Rust, the JavaScript connector needs explicit server-trust configuration.
Detail: [javascript/README.md](javascript/README.md).

## When resolution yields no service

Every accessor above reports a failed resolution as one error type carrying a
status, the requested capability and contract, what was looked for, and a
cause. Full table and per-language error shape:
[RESOLUTION.md, "When resolution yields no service"](RESOLUTION.md#when-resolution-yields-no-service).

| status | meaning |
| --- | --- |
| `runtime_unavailable` | No runtime could be selected, or the selected or explicit endpoint could not be reached. |
| `invalid_resolution` | The resolver answered, and the answer failed validation. |
| `unsupported_transport` | The resolver selected a reference whose scope or transport this binding cannot use. |
| `invalid_request` | The request was refused before it was sent. |
| the resolver's own refusal | `unavailable`, `forbidden`, `incompatible`, `unmet_requirements` or `not_ready`, from the resolution stage table above. |

The caller's own cancellation, and a waiting budget already spent when
resolution starts, stay the transport error (Python and JavaScript report a
pre-spent budget as `runtime_unavailable`).

On a platform the runtime declares unsupported (macOS, and Android including
Termux), resolving through the installed runtime reports `runtime_unavailable`
with the platform named in the error and its message, before any selection.
[RESOLUTION.md, "Unsupported platforms"](RESOLUTION.md#unsupported-platforms)
lists how each language detects it and shows how to read a refusal.

## Runtime

`openabstractions serve runtime` runs a locally built host in the foreground
for development. It composes logging, configuration and job hosts by default;
routing requires a separately registered routing provider. `--jobs-root` and
`--jobs-owner` enable recoverable job admission over the existing job provider.
An installed runtime is driven with `openabstractions start`/`status`; a
foreground host needs an explicit endpoint and independent server expectation
in the client. On Windows the default bootstrap endpoint is
`\\.\pipe\openabstractions-user-<process-token SID>-runtime-v1`; an endpoint
environment variable supplies an address, never installation trust. An absent
resolver or unavailable capability is an error — no accessor falls back to a
local provider.

Accepted job work stays with its original owner: persist the request identity
and binding, and reconcile an uncertain outcome against it rather than
resolving again. Cancelling a wait ends only the caller's wait;
`CancelWork`/`cancel_work` requests cancellation of accepted work separately.

## Migrating earlier callers

The Go `/legacy` package (`Jobs`, `Download`, `Storage`, `Log(program)`,
`Bindings`) was removed: replace `Jobs`/`Download` with `ResolveJobs` and a
submitted download request, and `Log` with `ResolveLog`. C++ convenience
accessors kept their spelling and now perform resolution; they can throw a
resolution error. Endpoint-only compatibility constructors
(`New`/`NewUnverified`) remain unverified; use them only as a deliberate
compatibility choice.

## What is unfinished

Per [RESOLUTION.md](RESOLUTION.md): release packaging, complete server trust on
every connector, and a waiting budget shared across subsequent capability calls
remain unfinished. Installed-runtime activation exists for the supported
Windows client paths; source and explicit-endpoint clients manage their own host
lifecycle. The Go `runtime` package is a user-runtime composition permitting
only its own service-account principal.
The independent C++ protocol target (`abstraction::facade_protocol`) is not
yet the installed runtime's resolver client. Storage resolution names a
resource without verifying it: hash
assembled bytes yourself. Rust `discover()` and a JavaScript `Machine`
with a null endpoint select installed-runtime identity through the shared C ABI
selector; explicit endpoints stay unverified unless a server expectation is given.

## Where it sits

Resolves [abstraction-logging](https://github.com/openabstractions/abstraction-logging),
[abstraction-config](https://github.com/openabstractions/abstraction-config),
[abstraction-router](https://github.com/openabstractions/abstraction-router),
[abstraction-job](https://github.com/openabstractions/abstraction-job),
[abstraction-storage](https://github.com/openabstractions/abstraction-storage),
[abstraction-asks](https://github.com/openabstractions/abstraction-asks),
[abstraction-rights](https://github.com/openabstractions/abstraction-rights) and
[abstraction-model](https://github.com/openabstractions/abstraction-model)
through one shared identity-bound transport
([abstraction-identity](https://github.com/openabstractions/abstraction-identity)).

One layer of [openabstractions](https://github.com/openabstractions/abstractions).
Every layer names one thing local tools rebuild on their own; the name means the
same in each language that implements it, and the conformance scenarios are what
hold an implementation to it.

## Requirements

Go 1.26 or newer. C++17 with CMake 3.16 or newer. Python 3.10 or newer for the
generated protocol package. Rust 2021 edition. Node 18 or newer for the
JavaScript package. Windows integration and CTest runs are exercised most;
RESOLUTION.md's Windows runtime test is the one described end to end.
[Agent adoption and contribution checks](CONTRIBUTING.md).

## Licence

Apache-2.0. See [LICENSE](LICENSE).

## Application directory and activation

The development runtime includes a permission-filtered directory of registered
applications and their leased live instances. An operator registers an exact
program descriptor. That program announces its workspaces and supported
interfaces. Closing an instance leaves the registered application available in
the directory. Each caller sees the applications it is allowed to read.

`ResolveApplications` binds `abstraction.facade/applications@1` in Go. Its
schema generates clients and dispatchers for all supported languages. The
current runtime serves local announcements with native program identity;
macOS access retains the documented Program-proof limitation. Interface
descriptions carry metadata. `Activate` is a separately authorized request for
the runtime to start the registered program and wait for its announcement.
Application-specific interface calls use their own contract and authorization;
the directory does not provide a generic invocation channel. See
[APPLICATIONS.md](APPLICATIONS.md) for the contract and limits.

Operators and users can inspect their permission-filtered view or request the
separately authorized activation through the generated client or CLI:

```console
openabstractions applications list --json
openabstractions applications activate <application-name> --json
```

The listing omits executable paths, start guidance and activation recipes.
