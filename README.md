# abstraction-facade

Ask for a capability without choosing its provider. The new service-client facade
provides logging, configuration and model-host routing. Applications call these
capabilities through IPC; separate Go services own provider behavior. Clients
contain no file sink or local fallback provider.

**Development API.** These examples describe current source, not a published
release or an installed OS service. The existing root Go package is a legacy
facade; its jobs, download and storage paths have not migrated to this API.

## Start the logging host

With a locally built `openabstractions` executable, run:

```sh
openabstractions serve logging --out ./logs/records.jsonl
```

This runs the logging host in the foreground. `--out` belongs to that host;
applications do not open the file. Starting this process does not register it
with the operating system's service manager.

Clients and host use the same default endpoint. `ABSTRACTION_LOG_ENDPOINT`
overrides the **framed** logging endpoint; the host also accepts `--endpoint`.
Use the same endpoint for both sides. This is distinct from the legacy
`ABSTRACTION_LOG_SERVICE` raw-record stream.

## Go

`Discover().Log()`, `Discover().Config()` and `Discover().Router()` select the
service client. Start the corresponding host with `serve logging`, `serve config`
or `serve router-v1`. Router's framed endpoint is separate from the legacy
`serve router` interface; GPU cost reporting remains on that legacy interface.

Use the service-only `/go/client` package:

```go
package main

import (
    "log"
    facade "github.com/openabstractions/abstraction-facade/go/client"
)

func main() {
    events := facade.Discover().Log()
    if err := events.Log(0, "worker started", map[string]string{"component": "worker"}); err != nil {
        log.Fatal(err)
    }
}
```

`Discover()` resolves lazily; it does not certify that a service is running.
The requested operation reports service absence as an error. `Log` supplies the
schema and timestamp so the application names only its event and attributes.

## C++17

Against locally built and installed development packages, point
`CMAKE_PREFIX_PATH` at their installation prefix:

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
        auto events = abstraction::facade::Discover().Log();
        events.Log(0, "worker started", {{"component", "worker"}});
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
```

The facade depends on `abstraction_logging`, `abstraction_config` and
`abstraction_router`, which use the shared
`abstraction_ipc` client runtime. CMake resolves installed dependencies or sibling
source checkouts; it does not fetch providers. The application gets a client,
not a logging server.

## What success means

Logging is one-way. A successful call confirms local transport submission, **not
a receipt that the host persisted the record**. Connection and encoding failures
are errors; an absent service never causes a client to create a local store.
See the [logging contract](https://github.com/openabstractions/abstraction-logging/blob/main/CONTRACT.md)
for the capability's semantics.

The new facade exposes `Log()`, `Config()` and `Router()`. The older import
`github.com/openabstractions/abstraction-facade/go` still supplies legacy
`Discover() (Machine, error)`, `Jobs()`, `Download()`, `Storage()`, `Log(program)`
and `Bindings()`. Those behaviors should not be read as guarantees of the new
service-client path. Python's generated logging protocol client is separate;
this page does not claim a Python facade or complete platform conformance.

[Adoption and contribution guidance](CONTRIBUTING.md) ·
[OpenAbstractions](https://github.com/openabstractions/abstractions) ·
[Apache-2.0 license](LICENSE)
