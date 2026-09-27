# Application directory experiment

`abstraction.facade/applications@1` is an experimental facade service defined
in `facade.thrift`. OA owns persistent application manifests and short-lived
instance leases. This service reuses the directory, native identity and rights
boundaries needed for application discovery. Evidence for a separate capability remains pending.
On the wire an application manifest is the struct `ApplicationDescriptor`, and
`Application` carries it in its `descriptor` field.

## Authority and lifetime

- `Register` and `Remove` require `abstraction.facade/application.manage` on
  `account`. A program cannot register itself. Registration grants no invocation,
  read, announce or activation permission.
- `Announce` and `Withdraw` require `abstraction.facade/application.announce`
  on `app:<name>`. The connection's account and program must own the manifest.
  Its kernel session must match the runtime session; a handle can only renew or
  withdraw within that session.
  Instances receive opaque handles within the current runtime epoch. Handle
  possession conveys no authority. Ownership is at program/account/session granularity.
- `Observe` filters each application using `abstraction.facade/application.read`
  on `app:<name>`. It returns a complete snapshot and rechecks permissions for
  each answer. Hidden application changes do not change a caller's cursor.
- `Activate` requires `abstraction.facade/application.activate` on
  `app:<name>`. Installation grants do not include it. The native connection
  supplies the caller account and program; request fields cannot select a user
  or session.
- Restart retains manifests and expires all presence. Renewal of an earlier
  instance returns `stale`. Observing a prior epoch returns `stale` with a fresh
  complete snapshot. Clients replace their snapshot; incremental continuity is
  never claimed.

Interfaces and contexts are attributed application metadata. They contain no
callable endpoint, launch command or independent grant. Placement `local`
(`scope=local` on the wire) records this directory's provenance. Remote
announcements are unsupported by this service and cannot be relabelled as local. Native identity uses the shared IPC
listener's Program proof; the current macOS limitation refuses access.

## Bounds and behavior

The directory holds at most 64 manifests and 128 live instances. A manifest
is at most 8192 JSON-encoded bytes; a presence is at most 2048. Per-field limits
are in the schema. Each presence contains up to 16 interfaces and 32 contexts.
Leases last 1–60 seconds; observation waits 0–30 seconds. The native host caps
frames at 1 MiB and concurrent calls at 64, with a 35-second call budget.
Directory-lock and persistence-lock waits honor cancellation. Synchronous kernel
staging, atomic replacement and sync can outlast that budget; the call waits for
their outcome and never reports cancellation followed by a detached late commit. State
mutations serialize within the service. The shared bounded CAS store persists
manifest additions and removals by synced atomic catalogue replacement.
A concurrent writer changing the root is refused. Failed persistence leaves the in-memory
manifest unchanged. A full directory returns typed `unavailable`.

A caller's changed epoch produces a full snapshot. Lease expiry establishes
absence of fresh presence evidence. Withdrawal, removal and observation never
start, terminate or restart an application. Start guidance is inert text.
Operator installation grants only management authority; applications and readers
need explicit per-application grants.

## Activation

`start_guidance` remains inert attributed text. An optional
`ApplicationActivationRecipe` is operator-registered typed data. It contains
literal arguments, one readiness interface and a 100–30000 ms total bound.
The recipe and the rest of its manifest together remain within the existing
8192-byte encoded manifest limit; it additionally permits at most 64
arguments of 1–4096 bytes each.

Activation first reuses a current manifest-program-bound presence whose
session and claimed interface exactly match the recipe. Otherwise one in-flight
attempt per application starts the registered program once. Concurrent callers join
that attempt. Readiness is a fresh identity-bound announcement with matching
interface metadata; this service does not probe the announced protocol.
The announcement proves the registered program and session. It does not prove
that the announcing process descended from the launch attempt. An already
running matching process can supply the fresh announcement; `started` records
that the platform launcher accepted the recipe, not process lineage.

The runtime launches only from a verified eligible graphical session. Windows
requires the caller and runtime kernel process session IDs to match, then routes
through the same-account desktop shell. Linux requires matching kernel audit
session IDs plus an unelevated process with `XDG_SESSION_ID`, `XDG_RUNTIME_DIR`
and a display or Wayland binding. The macOS applications service still refuses
native callers because its unix peer proof cannot bind a program; its launcher
also requires the current user's Aqua launchd domain. Other or unverified
placements return `forbidden` before authorization or `launch_refused` at the
launcher boundary.

Caller cancellation stops that caller's wait. It never signals the launched
process. A successful start that reaches the readiness deadline remains an
uncertain retained attempt, preventing a later call from silently starting a
duplicate. A later matching announcement is reusable. Removal disables an
in-flight attempt without terminating the application; replacing the manifest
cannot turn the older attempt into readiness for the replacement. The runtime
does not restart an application after process exit, withdrawal or lease expiry.
Executable inspection, platform launch and readiness share the recipe's one
waiting budget. A stalled inspection or launcher retains one uncertain attempt;
at most 64 active or retained attempts exist. Manifest removal without an
attempt releases its generation bookkeeping, bounding churn of unique names.
