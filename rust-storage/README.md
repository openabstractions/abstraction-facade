# Resolved storage content reader

This optional crate adds `StorageMachine` to the pure facade `Machine<C>`.
`resolve_storage(vec![], abstraction_facade_native::Scope::Local)` returns a fixed `Client<Binding<C>>`.
The base connector performs resolution, reference validation, waiting and framing.
Use the separate facade-native crate to select installed native IPC, or supply
another trusted Connector preserving the requested transport guarantees.

`open`, `read`, `close` retain explicit generated outcomes. Resources are opaque,
caller-scoped and live only in the selected provider. `copy` reads at most 64 KiB
per exchange under one composite wait budget, checks offsets/size/EOF, and returns
confirmed bytes on writer/call failure. Cancellation does not close a resource.
Close explicitly, including after revocation. `gap` requires an explicit reopen;
there is no retry or provider switching. Bytes are unverified: the digest names
requested content and callers verify assembled bytes before trusting them.

`resolve_storage_writer(vec![], abstraction_facade_native::Scope::Local)` returns a `Writer<Binding<C>>` for the
separate content-writer contract; every call remains subject to the service
write policy. `begin`, `append`, `commit` and `abort` validate inputs before any
exchange and check result consistency. `write(request, digest, bytes)` uploads
at most 64 KiB per append under one composite wait budget. Retain the request
identity from `new_request_id()` before calling: retrying the same identity
resumes a live upload or returns its committed result. `write` never aborts.

This crate depends only on the facade core and generated storage protocol. The
protocol depends on the pure shared frame contract. No local store implementation
or native library is required to compile the crate. Source version 0.0.0 is
current development packaging, not a registry release.
