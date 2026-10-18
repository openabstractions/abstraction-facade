# Application directory experiment

`abstraction.facade/applications@2` is an experimental facade service defined
in `facade.thrift`. OA owns persistent application manifests and short-lived
instance leases. This service reuses the directory, native identity and rights
boundaries needed for application discovery. Evidence for a separate capability remains pending.
On the wire an application manifest is the struct `ApplicationDescriptor`, and
`Application` carries it in its `descriptor` field.

Version 2 carries the `ApplicationEntry.self_registered` field. The published
version 1 record has three fields and refuses unknown fields. This runtime
advertises and serves version 2; a version 1 request receives `unknown_service`
and resolution of that exact older profile reports `incompatible`.

## Authority and lifetime

- `Register` and `Remove` require `abstraction.facade/application.manage` on
  `account`. A program cannot register itself unless the registration default
  is on: a `Register` whose descriptor names the caller's own program is
  decided on `abstraction.facade/application.register_self` on `account`
  instead, which the account-wide `register` default permits for every
  program of the account that has no rule of its own (rights RIGHTS-R22). A
  Block on that program, a script host, or the switch turned off refuses it.
  `application.manage` never covers the caller's own descriptor, and
  `register_self` never covers another program's. A title that reads as
  OpenAbstractions (`client.ReservedApplicationTitle`) is invalid, reason
  `title`, on both paths; an entry stored earlier keeps loading. A
  self-registered entry carries `self_registered` until the person answers
  Keep to the runtime's own native question, asked once at registration and
  again on `AskConfirmation` (asks ASK-R11); readers show it as
  "<title> (added itself)". The question names the program the platform
  proved for the registering connection, by that path rather than the
  descriptor's spelling, and shows the publisher bound from the same
  connection; a Remove blocks that program.
  Remove removes every unconfirmed entry of that program and blocks it from
  registering itself again. A program has one unconfirmed self-registration
  at a time: another one while it awaits the person is `conflict`, reason
  `unconfirmed`. The mark lives in `self-registered.json` beside
  `descriptors.json`, with the id of the question asked for that
  registration and the bound program and publisher; only the answer to that question
  acts on the entry, after a restart included. After a self-registration
  the runtime writes `application.read` and `application.activate` on
  `app:<name>` for each operator program that has no rule there, why
  `registered by the app`, so the Panel lists and starts it; a Block already
  there stays. Registration grants the registered program no invocation,
  read, announce or activation permission. An application's installer hands
  its manifest to a holder of that permission, the OA Panel, which registers
  it after the person confirms. That confirmation also has the Panel write
  `application.read` and `application.activate` on `app:<name>` for its own
  program in one conditional edit, with the person's click as its authority,
  so the person's control surface lists and opens what they registered; a
  confirmed `Remove` clears both rules. Installation names no app. The manifest file is the descriptor's wire
  form; `client.ParseApplicationDescriptor` reads it and
  `client.ValidApplicationDescriptor` is the one bounds check the service
  applies at `Register`.
- `Announce` and `Withdraw` require `abstraction.facade/application.announce`
  on `app:<name>`, which the account-wide `announce` default permits on every
  `app:<name>` unless the program's own rule or the switch refuses it. The
  connection's account and program must own the manifest. Ownership is
  decided before anything changes: a name another program owns and a name
  nobody registered both answer `forbidden`, and neither touches the entry
  or its activation attempt.
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

## Start log

The directory records each time a registered application becomes present:
the first instance it announces in a local session where none of its
instances is live. Each start carries the application's name, program and
title from its descriptor at that time, the time, and `launched`, true when
this runtime's own activation attempt for the application in that session was
waiting for it. A renewal, a second instance and a withdrawal record nothing.
The record is a fresh announcement, not process lineage; a runtime restart
makes every running application announce again. The log lives in
`starts.json` beside `descriptors.json`, oldest first, at most 256 starts,
each kept seven days; eviction past the bound records the time at or before
which a start may be missing. A failed write keeps the start in memory for
the next one and never refuses the announcement, and an unreadable log
starts empty, complete from the time it was opened.

`ListStarts(cursor, limit)` returns the log newest first, 1..64 starts per
page, to this runtime's operator programs only: the same check that admits
the asks operator (asks ASK-O1). Any other program, another account and a
remote peer get `forbidden`; a cursor is bound to the runtime epoch and the
log's content, and a changed log returns `stale`, restarting from an empty
cursor. The Panel's Activity timeline reads it. Tests:
`serve/application_starts_test.go`.

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
