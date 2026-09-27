# Pure Go facade core

Module: github.com/openabstractions/abstraction-facade/go-core. It holds the generated facade protocol types and the resolution/bootstrap packages the parent module's client package builds on. `go/client` imports these generated types; applications import `go/client`.

Install status: published; the modules in the tree pin `go-core/v0.2.0`, and `go get github.com/openabstractions/abstraction-facade/go-core@v0.2.0` fetches it. 0.3.0 tags `go-core/v0.3.0`.

Maintainers changing generated types or aliasing behavior: read
`research/facade/GO-CORE-NOTE.md` first.
