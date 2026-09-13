# abstraction-facade

Ask this machine for a capability. The facade resolves a compatible service and
returns a typed client. Applications use logging, configuration, routing or durable
jobs through shared IPC. Services own their data and execution.

**Development API.** This page describes the source at this revision. These
examples describe coordinated source builds. Verify the selected public revision
contains these APIs before adopting it; a released module may expose an earlier API. Python also has typed service clients; see [the Python package](py/README.md)
for its supported capabilities and native transport requirements. Rust and
JavaScript connectors require explicit server-trust configuration. Package
availability and platform qualification are recorded separately from source support.

## Go

Requirements: Go 1.26 or newer and a running compatible runtime.

The primary package is `github.com/openabstractions/abstraction-facade/go`.
`Discover` creates the resolver entry point. Each `Resolve*` call checks contracts
and guarantees and returns either a binding or an error.

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

The same context bounds resolution and this write. A reusable binding accepts a
fresh context for each later call. `ResolveConfig`, `ResolveRouter`, `ResolveJobs`
and `ResolveJobOperations` select their respective contracts. Requirements name
required guarantees and eligible scope. Unmet requirements return typed refusals.

For a released revision that exposes these methods, create an application module,
then select that exact facade revision:

```sh
go mod init example.com/my-app
go get github.com/openabstractions/abstraction-facade/go@<reviewed-revision>
go build .
```

Replace the placeholder with the commit or tag you reviewed. Module metadata
must resolve all dependencies at that revision. For coordinated development
checkouts, use a Go workspace containing the public modules; this tests source
compatibility and leaves release availability to a separate check.

## C++17

Use the [verified public revision set and complete example](cpp/examples/logging/README.md),
or build development packages using the [source installation instructions](cpp/README.md#build-and-install),
then give the resulting prefix to this outside application:

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
        auto events = abstraction::facade::Discover().ResolveLog();
        events.Log(0, "worker started", {{"component", "worker"}});
    } catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
```

`Log`, `Config` and `Router` resolve registered services with default requirements.
Use their `Resolve*` forms for explicit guarantees and scope. The shared transport
supports a deadline and cancellation token across resolution and service calls.
[C++ package and call lifetime](cpp/README.md).

An application using just durable jobs can link the smaller
`abstraction::facade_jobs` target. The [job consumer](cpp/test/jobs/README.md)
shows submission, observation and bounded result access.

## Runtime and ownership

The host installs and activates the shared runtime. An application discovers its
bootstrap address and asks the resolver for a service. For development, a locally
built `openabstractions serve runtime` runs that host in the foreground. The
default host registers logging, configuration and durable jobs. Routing requires
a registered routing provider.

Use `openabstractions start` and `openabstractions status` for an installed runtime.
Default Go/C++/Python selection verifies the independently registered runtime
account and program before resolver and provider payloads. A foreground host
requires explicit endpoint and independent server expectations in the client.
An endpoint environment variable supplies an address, not trusted installation evidence. Individual capability endpoint overrides do not choose the
facade's providers. An absent resolver or unavailable capability returns an error.
The application creates no local provider as a consequence of that error.

Accepted work stays with its original owner. Persist request identity and binding
recovery information and reconcile uncertain outcomes there. Cancelling a wait
ends the caller's wait; `CancelWork` requests cancellation of accepted work.
[Resolution contract](RESOLUTION.md).

Logging transport completion confirms submission of the frame. Durable logging
acknowledgment remains outside the current sink contract. Configuration values
and provenance are diagnostic settings; service storage stays behind service APIs.

## Migrating earlier callers

The root Go package now exposes service resolution. `Discover` returns `*Machine`
directly; resolution methods return errors. Earlier `Jobs`, `Download`, `Storage`,
`Log(program)` and `Bindings` methods belong to the explicit
`github.com/openabstractions/abstraction-facade/go/legacy` adoption package.
Selecting that package preserves earlier local-provider behavior and its weaker
lifecycle. It is a temporary migration choice for existing integrations.

The Go `/client` package uses the same resolved APIs. Replace its old `Log`,
`Config` and `Router` accessors with `ResolveLog`, `ResolveConfig` and
`ResolveRouter`. C++ convenience accessors retain their spelling and now perform
resolution; they can throw a resolution error before a capability call.

Explicit capability client constructors accept a deliberately supplied endpoint
and independent server expectation. Endpoint-only compatibility constructors
remain unverified; use them only as an explicit compatibility choice. Legacy provider APIs expose a different ownership model; keep their store and
lifecycle requirements explicit when maintaining an earlier integration.

[Agent adoption and contribution checks](CONTRIBUTING.md) ·
[OpenAbstractions](https://github.com/openabstractions/abstractions) ·
[Apache-2.0 license](LICENSE)
