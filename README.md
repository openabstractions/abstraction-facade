# abstraction-facade

**In development.** The example below runs, but this layer defines no
conformance rules of its own — see Status. No version number is typed on this
page: [the tag list](https://github.com/openabstractions/abstraction-facade/tags)
is the answer to "which release", because a tag is the only thing that cannot
drift.

For a Go application that wants jobs, downloads, storage and logging without
naming an implementation: one call reports what the machine it is running on
already provides and returns interfaces bound to that.

A tool that downloads a file, records it as durable work, avoids re-fetching
bytes another tool already holds, and writes a log line has four decisions to
make, and each has a different answer on a different machine: whether a job
service is installed, whether the operating system has a download tier such as
BITS, whether another tool already holds the same bytes, whether there is a
logging service or only a file. Today an application makes those decisions
itself and compiles in the answer. This layer makes them once, at startup, and
hands back interfaces, so the application never names a binding.

One layer of [Open Abstractions](https://github.com/openabstractions/abstractions),
the parent project, which holds the scope rules, the method, the measured
results and the conformance suite.

A **binding** is the concrete implementation behind an accessor — a file on
disk, a Windows service, an HTTP client. `Bindings()` names them for a
diagnostic line and nothing branches on them.

## Install

**Go.** The module path ends in `/go`; the package is `abstraction`.

```
go get github.com/openabstractions/abstraction-facade/go
```

[Releases, newest first](https://github.com/openabstractions/abstraction-facade/tags);
pin the exact tag you tested against, or `@main` for the tree as it stands.

**Python, C++.** None. This layer is Go only, so an application in either
language calls the layers directly — `abstraction-download`'s
[Python page](https://github.com/openabstractions/abstraction-download/blob/main/python/README.md)
is where a Python adopter starts.

Whether to adopt this at all, what it costs and what is not proven:
[Adopting](CONTRIBUTING.md#adopting).

## An example that runs

```go
package main

import (
	"fmt"
	"log"

	abstraction "github.com/openabstractions/abstraction-facade/go"
)

func main() {
	a, err := abstraction.Discover()
	if err != nil {
		log.Fatal(err)
	}

	downloads := a.Download()
	handle, err := downloads.Get("https://example.com/model.bin", "./model.bin")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("submitted job", handle.ID(), "via", downloads.Where())
	fmt.Println("bindings on this machine:", a.Bindings())
}
```

On a laptop with no supervisor running, no download tier linked into the
binary, and Ollama installed, that prints:

```
submitted job 1788929674223-467126fb9d3d2166f2ce via here
bindings on this machine: [jobs: file download: here (no system downloader, no tier linked) storage: ollama]
```

The id is the job's identity in the store: asking for the same URL and
destination again finds that job rather than starting a second one. The second
line lists what was found on that machine and is not a constant — reporting it
is what `Bindings()` is for. Nothing was installed to make this run.

`Get` returns as soon as the job is recorded; the transfer is not finished when
it returns. `handle.Wait(ctx)` blocks until the bytes are delivered and
`handle.Destination()` says where they landed. `downloads.Jobs()` is the live
collection, for a UI. `go/abstraction_test.go` submits a job through the facade
and reads the delivered file back.

## API

- `Discover() (Machine, error)` — reads what this machine has and returns a
  working `Machine`. Call it once, typically at startup.
- `Machine.Jobs() job.Store` — durable work of every kind on this machine, from
  [abstraction-job](https://github.com/openabstractions/abstraction-job).
- `Machine.Download() download.Client` — bytes in motion: submit fetches, watch
  progress, look one up by id. From
  [abstraction-download](https://github.com/openabstractions/abstraction-download).
- `Machine.Storage() storage.Store` — bytes at rest: whether this machine
  already has some content, and where new content should go. From
  [abstraction-storage](https://github.com/openabstractions/abstraction-storage).
- `Machine.Log(program string) logging.Sink` — where one program's log records
  go, from [abstraction-logging](https://github.com/openabstractions/abstraction-logging).
- `Machine.Bindings() []string` — names what is underneath each accessor.

Every accessor returns an interface, not a concrete type, so the binding behind
it can differ between machines and between runs without the application
changing.

## Status

Experimental, Go only, and no API stability promise before a 1.0. Its only
callers are our own adopters.

What `Discover()` returns is fixed at the moment it is called. Installing a
supervisor afterwards does not change what the process already holds; the next
`Discover()` sees it.

The layers underneath are at different points of maturity — some have several
bindings and integration tests against real services, others have one working
implementation. Read `Bindings()` rather than assuming a particular one is
present.

This layer defines no conformance rules of its own and no conformance scenario
cites it. `go/abstraction_test.go` is its only check; each layer underneath
carries its own.

## Requirements

Go 1.26 or later — `go/go.mod` declares `go 1.26.0`.

No OS-specific code in this package. The layers underneath may prefer a
particular operating system for a given binding (a BITS download tier is
Windows-only) and fall back elsewhere.

Below it:
[abstraction-job](https://github.com/openabstractions/abstraction-job),
[abstraction-download](https://github.com/openabstractions/abstraction-download),
[abstraction-storage](https://github.com/openabstractions/abstraction-storage)
and
[abstraction-logging](https://github.com/openabstractions/abstraction-logging).
Above it: the application.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
