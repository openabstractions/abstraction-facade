# Pure Go facade core

Module: github.com/openabstractions/abstraction-facade/go-core.
The canonical generated protocol is go/abstraction/facade; resolution/ and
bootstrap/ supply shared binding, receiving identity and current-user bootstrap
observations. Dependencies are identity and x/sys. Capability providers and the
aggregate runtime live in the separate parent go module.

Original go/abstraction/facade types remain aliases with real forwarding codec
functions. Original resolution/bootstrap packages forward to this core, preserving
Go type identity. Generated exported slice rosters initially share their backing
contents. Reassigning a roster variable affects that package variable alone;
clients should treat generated metadata as read-only. No exported mutable scalar
state was moved, and alias generation refuses that unsupported case.

The core module is new development source. v0.0.0 requirements in dependents are
explicit placeholders; they do not identify available public versions. Publish
and qualify a real core version first, replace dependent placeholders with that
version and genuine sums, then qualify/tag dependent modules. The release checker
--require-published-pins refuses the placeholder graph. Local source verification
uses private go.work or isolated candidate replacements; public go.mod files carry
no filesystem replacement.
