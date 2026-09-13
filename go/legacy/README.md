# Explicit legacy adoption

Import `github.com/openabstractions/abstraction-facade/go/legacy` to retain the
previous `Discover() (Machine, error)`, `Download`, `Jobs`, `Storage`, `Log` and
`Bindings` APIs. This deprecated package may create local stores, run downloads
inside the application and choose file logging. Its implementation preserves
those existing semantics under an explicit import.

The parent package now provides `Discover() *Machine`, `New(endpoint)` and
`ResolveLog`, `ResolveConfig`, `ResolveConfigEditor`, `ResolveRouter`, `ResolveJobs`, and
`ResolveJobOperations`. Pass a context and `Requirements{}` for contract defaults.
Resolve a service, then call its typed operations. Preserve job identity, endpoint
and owner for recovery; use service result reads for completed bytes.

The panel defaults to service clients. Its explicit legacy mode retains old
provider integration for compatibility. Moving an application's import to this
package preserves its earlier behavior and leaves its service migration pending.
