# abstraction.facade contract

Binds: `facade.thrift`

`facade.thrift` defines `abstraction.facade/endpoint@1`, which every
generated endpoint serves, and `abstraction.facade/registry@1`, the one
registry of the programs a runtime knows, which feeds its resolution
catalogue and its router (research/provider-registry/DECISION.md). It also
defines resolution ([RESOLUTION.md](RESOLUTION.md)) and the caller echo.
[README.md](README.md) holds the walkthrough: resolving a service, reading
the registry, and registering a provider.

## Reading this page

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD",
"SHOULD NOT", "RECOMMENDED", "MAY" and "OPTIONAL" in this page are to be
interpreted as described in RFC 2119 and RFC 8174, when, and only when, they
appear in all capitals, as shown here.

A rule id such as `FAC-B1` is declared once, in bold brackets before its
title, at the head of the rule it names; tests and refusals cite it the same
way. A retired id is never reused; [HISTORY.md](HISTORY.md) keeps it with the
release it left. `HISTORY.md` also carries this contract's earlier drafts and
what was measured, linked from here and linking back.

| letter | topic |
|---|---|
| B | binding (`endpoint@1`'s description and the resolved reference) |
| R | registry (`registry@1`) |

`A` (admission), `E` (error and outcome) and `X` (extension) are reserved
with one meaning in every contract (S12) and are not separately declared
here: every rule below states its own admission and outcome inline.

**Prose-to-wire.** The words below are the decided names
(`research/vocabulary/DECISION.md` D4, D15, D42); the wire still uses the
name on the right until the release named ships
(`research/vocabulary/RENAME-PLAN.md` §4), and the stored-policy reader
accepts both names for that release where one applies.

| decided name | current wire name, until it ships |
|---|---|
| `service` (the `abstraction.x/y@n` string) | `contract` — first release |
| `placement` | `scope`/`Scope` — first release |
| `register` | `Declare` — third release |

## Binding

**[FAC-B1] Description.** Every Go and C++ dispatcher generated for a
service with a request-response method MUST answer `Describe` (idl/LANGUAGE.md
DEF-S2). An endpoint hosting several services MUST list every one, in its own
order. A service reads `ready` unless its handler's readiness hook reports
otherwise, and then `not_ready` with the hook's reason in `why`.

**[FAC-B2] No self-reported authority.** `program` and `version` MUST be the
provider's own words and never authority: the caller binds the server by its
connection proof. A registry MUST require the registered program as the
server before it reads a description.

**[FAC-B3] Unknown services.** An endpoint built before this service MUST
answer `unknown_service`. A frame for any other undeclared service MUST
still read `unknown_service`.

**[FAC-B4] The resolved reference.** Every language client MUST expose the
reference of a resolved binding, read-only, under the name `reference`: Go
`Binding.Reference()`, Python `Binding.reference`, JavaScript
`binding.reference`, C++ `BoundService::reference()` and Rust
`Binding::reference()`. It carries the resolver's own `ServiceReference`
under the names [facade.thrift](facade.thrift) gives them: `provider`,
`capability`, `contract`, `guarantees`, `scope`, `transport` and `endpoint`.
`provider` names the logical provider that answered this resolution, never a
PID, a program path or a filesystem location, and an application displays it
to say which provider served it. The reference MUST grant nothing: it is
neither an authorization token nor proof that the endpoint is still there,
and the receiving boundary MUST recheck authority, identity, compatibility
and the required guarantees when it accepts work
([RESOLUTION.md](RESOLUTION.md)). Reading it MUST make no call. Each read
MUST yield an independent copy, so a caller that edits what it read changes
no binding. A binding a caller restored from a retained endpoint carries no
reference and reads as the language's absent value; a C++ `BoundService` is
constructed from a reference and always has one. A client that binds a
service with no typed capability accessor uses `ResolveService` (Go),
`resolve_service` (Python, Rust, C++) or `resolveService` (JavaScript), which
returns that binding.

## Registry

**[FAC-R1] Registration file.** A registration MUST be `providers/<name>.json`
in the runtime state, version 2, written by `Declare` and read at start,
identified by its name and kept until withdrawn; a reading MUST be computed
by the runtime and never stored. It keeps `role`, `resources`, `models`,
`remote` and `host`. `declared_by` names the bound caller's program for a
registration `Declare` wrote. A version 1 file, which carried `stores` and
`profiles`, MUST be rewritten once at start: each store becomes
`store:<name>` and each profile `profile:<name>`. Each `hosts.json` hosted
entry of wire `oa-remote@1` becomes a remote registration serving
`abstraction.inference/chat@1` and `abstraction.router/router@1`, with its
credential in `remote.credential`. Every other `hosts.json` entry, local or
hosted, becomes a registration of role `host` keeping its `declared_by` as
the registration's provenance and its credential's ceiling in
`host.ceiling`. All of them leave `hosts.json`, which keeps its ceilings, its
bounds and its `declared` switch, written out explicitly so the products
keep registering what they registered before. Registrations MUST be written
before `hosts.json`, so an interrupted rewrite resumes on the next start.

**[FAC-R2] Local providers.** Every probe MUST call `endpoint@1` `Describe`
over a connection requiring the registered program as the server. A process
running another program reads `refused` with why `program:<detail>`. A
failed Describe reads `unreachable` with why `describe:<code>`,
`describe:unknown_service` for a provider built before `endpoint@1`. A
registration reads `ready` when the description lists every registered
service ready. A service absent or not ready reads `not_ready` with why
`contract:<wire name>:<reason>`, and its candidates are withdrawn. An
`on_demand` provider MUST launch when a resolution it would satisfy finds it
not ready, and MUST relaunch after an exit with a backoff of 250 ms doubling
to 30 s, reset after a minute up. A program MUST NOT register itself.

**[FAC-R3] Resource acceptance.** Each capability MUST read its own
acceptance rule for a registration's resources. A `store:<name>` resource
belongs to an inventory source, which names at least one. At each probe the
runtime MUST accept a store the source describes only when the registration
names it and the rule permits it, and MUST list it in `accepted`. An
on-demand source launches when the runtime starts serving, because the
runtime is its only reader. The same rule covers every on-demand
registration whose services the runtime reads on its own behalf: a native
inference provider that names its profiles is launched when the runtime
starts serving, because the router lists its models, and one that names none
stays lazy under FAC-R8 (decided 2026-09-22; no third activation value,
since what changes is who reads, not how the process starts). A native
inference registration's `models` are a trusted routing allowlist; `chat@1`
and `endpoint@1` provide no model inventory. The list accepts common
provider names such as `owner/model` and MUST NOT confer a rights grant. A
registration asks for no rule by name: it names the resources it will use,
and the capability that owns a resource kind MUST write the permit rule its
contract needs at `Declare`, with why `provider add`, leaving an existing
rule as it is: `store:<name>` takes `abstraction.storage/inventory.provide`;
a resource of the table (`card:<n>`) takes `abstraction.resource/hold` for
the registered program. The operator who registers holds `provider.manage`,
and the rule written is the grant that registration implies; withdrawing the
registration leaves the rule for the operator to revoke, as with stores.

**[FAC-R4] Remote runtimes.** A remote runtime MUST be a registration of
transport `oa-remote@1`, activation `remote`, endpoint `tls://<host>:<port>`,
no program, and `remote` naming the server name, the PEM files of the
trusted roots and this runtime's client certificate and key, and the
credential the remote holds. `Declare` MUST refuse trust files it cannot
load as `invalid` with reason `remote`, and MUST write
`abstraction.inference/complete` on `host:<name>` for the runtime's operator
programs and the caller. Its readiness is its `endpoint@1` Describe over
mutual TLS. The router reads the remote's `router@1` `Models` for the
server's listing and its `Hosts` as `<name>/<host>` with `domain` `<name>`,
and a `chat@1` operation picked for it is delegated as the inference
CONTRACT's "Remote runtimes" section states. No registry is read across
machines.

**[FAC-R5] Registry admission.** `registry_actions` is closed:
`abstraction.facade/provider.manage` on resource `account` MUST decide every
registry call for the bound caller. The default installation grants it to
the runtime's operator programs. A caller without it reads `forbidden`, and
a decision point that cannot answer reads `unavailable`. Edits MUST be
conditional on the revision, the digest of every registration file the
operator and the installation hold and of the disabled names. `Observe`
answers when the digest of the registrations and their readings differs from
the cursor, or when `wait_ms` (0..30000) ends.

**[FAC-R6] Roles.** A registration's `role` MUST be what it is, and each
role MUST validate its own fields. `provider` names a managed process: a
program, an endpoint, transport `oa-native@1`, activation `on_demand` or
`attach`, and its services. `remote` names a remote runtime: transport
`oa-remote@1`, activation `remote`, endpoint `tls://<host>:<port>`, the
trust record and no program. `host` names a model server, a foreign HTTP
engine: transport `http@1`, activation `attach`, `host` naming the base URL,
the wire kind, whether it is hosted, the credential and its ceiling, and no
program, endpoint, service, guarantee, model or trust record. A registration
that names no role reads its shape: `oa-remote@1` is `remote`, `http@1` is
`host`, anything else is `provider`. A role that denies the registration's
transport MUST read `invalid` with reason `role`. A model server
registration is no process: no supervisor probes it, and its reading in
`DeclarationState.host` is the router's last survey, with readiness `ready`
when the survey reached it and `unreachable` with why `host:<detail>` when
it did not. `abstraction.inference/operator@1` `Hosts`, `AddHost` and
`RemoveHost` are fronts over these registrations under their own action,
`host.manage` (inference CONTRACT.md INF-H3).

**[FAC-R7] Reading registrations.** The runtime MUST read registration files
from the directory `declarations` beside the installed runtime executable,
the rule the installation's other programs already follow:
`<install>\tools\declarations` on Windows in a per-user or machine-wide
install, and `~/.local/bin/declarations` on Linux and macOS. It MUST assert
`declared_by` `installation` for each, whatever the file says. A provider
registration read from this directory MAY name its program as a bare file
name carrying no path separator, because an absolute path cannot be written
into a file the installer ships; the runtime MUST resolve it against the
tools directory the `declarations` directory sits beside, the same directory
the installation's other programs already resolve against, and that
resolved absolute path is what the registration reads, what the runtime
launches and what a rights rule names. A bare name resolving to no file on
disk MUST leave the registration unread, reported `invalid program` and
naming both the shipped name and the resolved path. A registration written
by `Declare`, in the operator's own directory, keeps the absolute-path rule;
only a registration read from the installation's directory resolves a bare
name. The model servers a product's own record registers (inference
CONTRACT.md INF-H1) are read the same way, with the product's word as
`declared_by`, and are written nowhere. An operator's registration of a name
shadows a product's, which shadows the installation's; `Declare` of a name
an enabled registration already holds reads `conflict` with reason `name`.
`Withdraw` of a registration the installation or a product made MUST record
the name in `declarations-disabled.json` in the runtime state instead of
removing the file: the reply is `applied` with reason `disabled`, the
registration reads `disabled` with why `operator`, the runtime offers none
of its candidates and the router reaches none of its servers, and a
reinstall or a later probe does not bring it back. `Declare` or `AddHost` of
that name forgets the record and enables it again.

**[FAC-R8] Activation.** `Resolve` MUST never block: a candidate's
activation is asynchronous, as the resolver contract says. The first
mediated call that needs an on-demand provider (a lend, a native inference
call through a registered provider) MUST wait for its readiness inside the
caller's own deadline and never longer than the runtime's launch budget of
30 s; that wait is part of the mediating capability's contract and no
capability hides it. A caller whose deadline passes first reads that
capability's `unavailable` with reason `activating:<name>`, the launch
continues, and the next call finds the provider ready or `not_ready` with
its reason. A provider that is `ready` costs a call nothing beyond the
description read. `attach` probes a provider something else runs. A native
`abstraction.inference/chat@1` registration with an explicit model allowlist
is a mediated local resolver candidate after the runtime's own, with the
registration's name as provider, its guarantees plus the local-only
guarantee, and an OA endpoint pinned to that registration's revision. Other
registered services remain visible to operators and expose no application
endpoint until their capability owns mediation. `Withdraw` MUST remove the
candidates and end a launched child. A revision's OA endpoint MUST stay
reachable while chat operations selected through it remain active or
retained under the inference contract; it MUST refuse every new Start after
withdrawal and MUST close when those operations retire. At most 64 active
and retained revision endpoints exist, so a new candidate remains not ready
until a retained endpoint closes.

## Outcomes

| service | call | outcomes |
|---|---|---|
| `endpoint@1` | `Describe` | `DescriptionOutcome`: `forbidden`, `unavailable`, `invalid` |
| `registry@1` | `Declarations`, `Observe` | `DeclarationListOutcome`: `forbidden`, `unavailable`, `invalid` |
| `registry@1` | `Declare`, `Withdraw` | `DeclarationEditOutcome`: `forbidden`, `unavailable`, `invalid`, `conflict` (a stale revision, name or endpoint); `Withdraw` also `unknown` |

A reading's `why` carries the typed cause as `<source>:<detail>` from
`program`, `describe`, `contract`, `remote`, `host`, `launch` and `exited`,
or the word `operator` for a disabled registration; no cause enum is
declared.

## Bounds

At most 64 registrations of every role and source together, 16 services and
guarantees, 64 arguments and resources each; a listing fits one 1 MiB frame.
`DeclarationTransport` and `DeclarationRole` are the closed registration
transport and role choices; `declaration_resource_kinds` grows with the
runtime that reads it; `registry_actions` is closed by FAC-R5. `capabilities`
in a description is an open map of display facts.

## Divergences

None declared for this release.

## Not built

Release packaging, complete server trust on every connector, and a waiting
budget shared across subsequent capability calls are not built (README.md,
RESOLUTION.md). The registry mediates no compute: it starts and stops the
processes registrations name and confers no rights grant beyond the permit
rules FAC-R3 states.
