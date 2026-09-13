# Python resolved capability clients

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
from abstraction.facade.client import Machine
from abstraction.logging.rec import Record

machine = Machine(deadline=time.monotonic() + 2)
sink = machine.resolve_log(scope="local")
sink.Write(Record(schema=1, time="2026-09-12T12:00:00.000000Z", level=1,
                  msg="connected", attrs={"component": "example"}))
```

`resolve_log` checks the returned capability, exact contract, requested guarantees,
scope, endpoint and transport before binding. `ResolutionError.status` retains a
typed refusal. Only local `oa-framed-local@1` references are supported. A selected
client keeps its endpoint and waiting policy; failed calls are not reselected or
replayed. Write completion confirms local transport submission, not durable log
storage. Provider errors on a one-way call remain service-side diagnostics.

The caller can pass a shared cancellation signal. A fixed deadline includes time
between resolution and use; create another Machine for a later budget. Default
bindings use fresh per-call timeouts. The consumer supplies no identity claims;
the production receiving boundary establishes caller evidence.

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

Select source revisions that include these development APIs. The package's
0.0.0 metadata supplies local installation, with no published-version claim.
Logging types remain in the separate `abstraction-logging-protocol` package.
The base facade depends on shared IPC. Select `logging`, `config` or `jobs`
extras for the capability types you use; `all` installs all three. Each capability
is imported when selected. A jobs application can install the identity, job and
facade sources with the `jobs` extra. Add the download request package when
submitting download work. Logging and configuration packages are independent.
The focused fixture installs these actual source packages offline into a clean
prefix and requires no synthetic package metadata. Native artifact release
coordination and other platform evidence remain
unfinished. No local provider fallback is supplied.

`resolve_config` and `resolve_config_editor` bind the generated ConfigReader and
ConfigEditor. They preserve the same resolver validation, fixed endpoint and
waiting policy. See the config `py/README.md` for explicit run overrides and
revision conflicts. Existing file records remain behind the service provider;
application code does not follow provenance paths to perform storage work.

Durable work uses `resolve_jobs`, `resolve_job_operations` and the separate
`resolve_job_inventory` binding. See the job Python README for caller-retained
recovery information, validated receipts, CopyResult partial failures and
with_waiting policy. These clients never rediscover accepted work automatically.
`Jobs.restore_installed(endpoint, owner, ...)` verifies retained work against the
current installed runtime and its persisted logical owner. Explicit restoration
uses `Jobs.restore(..., server=expectation)`. Waiting-policy copies retain trust.
