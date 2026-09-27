# Python resolved capability clients

Install status: source only today — build the native IPC library locally, then `pip install` from local paths; 0.3.0 plans a PyPI wheel, pending owner consent.

Python clients resolve logging, configuration and durable work through generated
interfaces over `abstraction.ipc`. An operator configures a shared-library path or
prefix. `Machine()` selects the installed runtime's program and account through
the shared native library and verifies that identity at resolver and provider
connections. `ABSTRACTION_RUNTIME_ENDPOINT` changes the connection location while
preserving installation trust. Services retain their private state.

Explicit endpoints support deliberately configured providers. Supply
`server=ServerExpectation(...)` with independently configured program/account
evidence. Endpoint-only construction retains unverified compatibility behavior.
An optional `provider_trust(reference)` selects independently configured trust
for providers in other executables. Catalogue labels carry no identity authority.

```python
import time
from abstraction.ipc import Library
from abstraction.facade import Scope
from abstraction.facade.client import Machine
from abstraction.logging import Record

machine = Machine(deadline=time.monotonic() + 2)
sink = machine.resolve_log(scope=Scope.LOCAL)
sink.write(Record(schema=1, time="2026-09-12T12:00:00.000000Z", level=1,
                  msg="connected", attrs={"component": "example"}))
```

`resolve_log` checks the returned capability, exact contract, requested guarantees
(a guarantee is a contract-specific promise; see the [reference vocabulary](https://openabstractions.org/reference.html#vocabulary)),
scope, endpoint and transport before binding. Every resolve call that yields no
service raises `ResolutionError`. Its `status` is the resolver's refusal, a
validation refusal, or `runtime_unavailable` when no runtime could be selected or
reached. `capability`, `contract` and `looked_for` (the installed runtime or the
explicit endpoint) name the request, and `__cause__` holds the transport error.
The caller's own cancellation remains a `FrameError`. There is no fallback
provider. Only local `oa-framed-local@1` references are supported. A selected
client keeps its endpoint and waiting policy; failed calls are not reselected or
replayed. write completion confirms local transport submission, not durable log
storage. Provider errors on a one-way call remain service-side diagnostics.

## Naming the provider that served you

`machine.resolve_service(contract)` binds one exact versioned contract identity
and returns a `Binding`. `binding.reference` names the provider that answered,
read-only, so an application can say which provider served it (CONTRACT.md
`FAC-B4`). It is the resolver's `ServiceReference`: `provider`, `capability`,
`contract`, `guarantees`, `scope`, `transport` and `endpoint`. Each read returns
an independent copy, and the reference grants nothing.
`reference(client)` reads the same reference from a capability client the
facade built, and returns `None` for a client built any other way.

```python
from abstraction.facade import Scope
from abstraction.facade.client import Machine, reference
from abstraction.storage.content.client import Client

binding = Machine().resolve_service("abstraction.storage/content-reader@1", scope=Scope.LOCAL)
reader = Client(binding)
print("reused from", reference(reader).provider)
```

The caller can pass a shared cancellation signal. A fixed deadline includes time
between resolution and use; create another Machine for a later budget. Default
bindings use fresh per-call timeouts. The consumer supplies no identity claims;
the production receiving boundary establishes caller evidence.

`machine.resolve_applications()` binds the experimental local application
directory using the same verified transport. Its generated client provides
`observe`, `announce`, `withdraw` and explicit `activate` calls. The operator
must register the application and grant the relevant actions first. Announcements
are attributed to the calling executable, account and session. An interpreter
process is identified at that executable's granularity. Use a sufficient per-call
timeout, such as `Machine(timeout=35)`, when waiting for a bounded activation.
See [the application profile](../APPLICATIONS.md) for authority and lease rules.

## Install a coordinated source checkout

Use Python 3.10+, pip/setuptools, CMake and a native C++ compiler already installed.
From the common parent of public identity, facade and logging repositories, build
and install the native transport, then install the logging capability packages:

```sh
PREFIX="/absolute/writable/ipc-prefix" # on Windows use an absolute drive path
cmake -S abstraction-identity/cpp -B build/ipc -DBUILD_SHARED_LIBS=ON -DABSTRACTION_IPC_BUILD_TESTS=OFF -DCMAKE_INSTALL_PREFIX="$PREFIX"
cmake --build build/ipc --config Release
cmake --install build/ipc --config Release
python -m venv .venv
# Activate the venv using your shell's activation command, then:
python -m pip install ./abstraction-identity/py ./abstraction-logging/py "./abstraction-facade/py[logging]"
export ABSTRACTION_IPC_PREFIX="$PREFIX" # Bash; use your shell's environment syntax
```

Select source revisions that include these development APIs. Logging types
remain in the separate `abstraction-logging-protocol` package.
The base facade depends on shared IPC. Select `logging`, `config`, `jobs` or
`storage` extras for the capability types you use; `all` installs all four. Each capability
is imported when selected. A jobs application can install the identity, job and
facade sources with the `jobs` extra. Add the download request package when
submitting download work. Logging and configuration packages are independent.
The focused fixture installs these actual source packages offline into a clean
prefix and requires no synthetic package metadata. Native artifact release
coordination and other platform evidence remain
unfinished. No local provider fallback is supplied.

`resolve_config` and `resolve_config_editor` bind the generated ConfigReader and
ConfigEditor. They preserve the same resolver validation, fixed endpoint and
waiting policy. See the config layer's [Python README](https://github.com/openabstractions/abstraction-config/blob/main/py/README.md) for explicit run overrides and
revision conflicts. Existing file records remain behind the service provider;
application code does not follow provenance paths to perform storage work.

Durable work uses `resolve_jobs`, `resolve_job_operations` and the separate
`resolve_job_inventory` binding. See the job layer's [Python README](https://github.com/openabstractions/abstraction-job/blob/main/py/README.md) for caller-retained
recovery information, validated receipts, copy_result partial failures and
with_waiting policy. These clients never rediscover accepted work automatically.
`Jobs.restore_installed(endpoint, owner, ...)` verifies retained work against the
current installed runtime and its persisted logical owner. Explicit restoration
uses `Jobs.restore(..., server=expectation)`. In Go this operation is
`RestoreJobs`; in C++ it is client reconstruction; in Rust it is `Jobs::restore()`.
Waiting-policy copies retain trust.
