# Pure Go facade core

*For maintainers of the Go facade.*

Module: github.com/openabstractions/abstraction-facade/go-core. It holds the generated facade protocol types and the resolution/bootstrap packages the parent module's client package builds on. `go/client` imports these generated types; applications import `go/client`.

The `protected` package carries a protected operation between the runtime,
the operator presenter and the elevated helper: the runtime's ticket broker,
the helper's exchange and the Windows launch through the administrator prompt
([CONTRACT.md](../CONTRACT.md) FAC-H1..FAC-H4).

Install status: published; the latest tag is `go-core/v0.3.0`, and `go get github.com/openabstractions/abstraction-facade/go-core@v0.3.0` fetches it.

Maintainers changing generated types or aliasing behavior: read
`research/facade/GO-CORE-NOTE.md` first.
