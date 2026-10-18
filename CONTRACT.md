# abstraction.facade contract

Binds: `facade.thrift`

`facade.thrift` defines `abstraction.facade/endpoint@1`, which every
generated endpoint serves, and `abstraction.facade/registry@1` and `registry@2`,
the two wire views of one registry of the programs a runtime knows, which feeds its resolution
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
| R | registry (`registry@1` and `registry@2`) |
| P | process supervision (the Go `supervision` package that starts provider and engine processes) |

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
not ready, except a native inference candidate resolved to the runtime-owned
mediator: resolution returns that mediator without starting its downstream
provider. The downstream provider launches only when an inference request
passes preflight and reaches native admission under FAC-R8. An on-demand
provider MUST relaunch after an exit with a backoff of 250 ms doubling
to 30 s, reset after a minute up. A program MUST NOT register itself.

**[FAC-R3] Resource acceptance.** Each capability MUST read its own
acceptance rule for a registration's resources. A `store:<name>` resource
belongs to an inventory source, which names at least one. At each probe the
runtime MUST accept a store the source describes only when the registration
names it and the rule permits it, and MUST list it in `accepted`. An
on-demand source launches when the runtime starts serving, because the
runtime is its only reader. A native inference provider is not launched
merely because the runtime starts serving or the router lists its models.
The router can list declared models and profiles while it is idle, with
residency unknown. Resolution of its native inference candidate reaches the
runtime-owned mediator without launching the downstream process; an inference
request refused before admission cannot start it. A registration that also
serves an inventory source still follows that source's eager rule. This replaces
the 2026-09-22 profile-based eager exception because the declaration now
supplies the router's model inventory. A native
inference registration's `models` are a trusted routing allowlist; `chat@1`
and `endpoint@1` provide no model inventory. The list accepts common
provider names such as `owner/model` and MUST NOT confer a rights grant.
Entries are read against the profiles the registration serves: its listed
`profile:` resources whose contracts it declares, or `chat` for a
registration that lists none and declares `abstraction.inference/chat@1`.
An entry `<profile>:<model>` whose profile it serves serves `<model>` for
that profile alone, every other entry serves every served profile, and a
model literally named `chat:x` is written `chat:chat:x`
(`abstraction-router` CONTRACT.md ROUTE-D9). The runtime MUST refuse a
registration with an entry scoped to a listed profile whose contract it
does not declare, as `invalid` with reason `models`. A
registration asks for no rule by name: it names the resources it will use,
and the capability that owns a resource kind MUST write the permit rule its
contract needs at `Declare`, with why `provider add`, leaving an existing
rule as it is: `store:<name>` takes `abstraction.storage/inventory.provide`;
a resource of the table (`card:<n>`) takes `abstraction.resource/hold` on it
and on `awake` for the registered program, because a card holder runs GPU
work and holds the wake lease while it does (`abstraction-resource`
CONTRACT.md RES-A1, RES-G5). Every way of registering can name a card:
`openabstractions provider add` takes it as `--card <n>`. The operator who
registers holds `provider.manage`,
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

A provider or remote runtime MAY declare up to 64 distinct
`supported_extensions` entries, each a profile and an inference extension
key. The profile MUST be one it serves through the corresponding declared
inference contract; the key MUST have the bounded `<owner>/<name>` shape
of an inference request extension. The reserved caller-claim key MUST NOT
be declared. This operator-managed assertion travels with the declaration's
registration identity to the selected router host. An absent entry means a
required key is unsupported before the provider is activated. A selected
service still decides whether a value or model-specific use is supported;
the assertion does not authorize the request or grant rights. A foreign HTTP
host has no such inventory.

The runtime MUST advertise `registry@1` and `registry@2` on the same
authenticated local endpoint and give them the same readiness lifetime.
Both profiles MUST decide `provider.manage` for the bound caller on every
call. They share one declaration revision, registration generation, edit
conflict rules and withdrawal authority. `registry@2` `Declare` MUST validate
the base declaration and extension inventory together and store both as one
generation. Its `Declarations` and `Observe` MUST read each base declaration
and its inventory from the same registration snapshot. The `registry@2`
observation cursor includes the inventory. `registry@1` keeps its original
strict wire shape: its `Declaration` and readings omit extension inventory.
Its `Declare` creates an empty inventory. `Declare` in either profile adds a
name only when available; it cannot update an existing generation. `Withdraw`
in either profile withdraws the whole generation, including its inventory.

For a local `provider`, the declared endpoint is a short service name. The
runtime MUST expand that name to its platform-local pipe or socket address
for probing, mediation and every `{endpoint}` launch argument. A provider MAY
call the shared `listen.Endpoint` constructor on that argument; an already
qualified local pipe or absolute socket address stays unchanged. A remote
pipe path is not a qualified local endpoint. Explicit endpoint constructors
remain available for a separately configured provider process.

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
description read. `attach` probes a provider something else runs. A child
the runtime launches MUST receive the runtime's own service endpoints in the
variables their clients read: `ABSTRACTION_RESOURCE_ENDPOINT`,
`ABSTRACTION_LOG_ENDPOINT`, `ABSTRACTION_CONFIG_ENDPOINT`,
`ABSTRACTION_ROUTER_ENDPOINT`, and `ABSTRACTION_RUNTIME_ENDPOINT` when the
runtime does not listen at the installed endpoint, so a declaration names
none of them; an argument the declaration carries overrides the variable
for a provider whose service lies elsewhere. The child MUST NOT inherit any
of these five variables the runtime does not set, since a value from the
runtime's own parent would lead the provider to another runtime: a runtime
at the installed endpoint sets no `ABSTRACTION_RUNTIME_ENDPOINT`, and a
runtime on installed Mach services (XPC) sets none of the five, because
its clients find those services by name. Every other variable of the
runtime's environment passes to the child unchanged. A native
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

## Process supervision

`go/supervision` starts a provider or engine process inside its own job
object on Windows, assigned in `CreateProcess` before the child runs
(`PROC_THREAD_ATTRIBUTE_JOB_LIST`), and ends its whole tree.

`Options.Args` uses Windows `CommandLineToArgvW` quoting. A caller whose
program parses its command line differently, such as `cmd.exe` or a batch
launcher, supplies the full `Options.WindowsCmdLine` with empty `Args`;
`Start` refuses both together before creating a process. The command line
is passed unchanged, while `Options.Executable` remains the explicit
`CreateProcess` application name. Outside Windows `Start` returns
`ErrUnsupported` for either form.

**[FAC-P1] Inherited handles.** The child inherits exactly the handles its
caller names in `Options.Inherit` and its three standard handles, all named
in one `PROC_THREAD_ATTRIBUTE_HANDLE_LIST`; no other inheritable handle of
the starting process reaches it. Each named handle MUST be open, marked
inheritable by its caller, and named once. `Start` refuses any other list
with `ErrInherit` before it creates a job or a process, and it never
changes a caller's handle. The child receives each handle at the value the caller holds, and
the caller keeps its own handle and may close it once `Start` returns. The
job assignment stays part of `CreateProcess`. A provider host lends a model's
read-only memory section to an engine this way
(abstraction-inference CONTRACT.md INF-MS4). Outside Windows `Start` is
unsupported.

**[FAC-P2] The tree's exit is observed.** Once `Wait` has returned,
`Process.Exit` reports what the job saw of the ended tree: the parent's
exit code (a crash's exception status, such as `0xC0000005`, read as a
named status by `Exit.Status`), its run time from its process times,
whether `Stop` ended it (`Stop` ends the tree with exit code `0x4F415354`,
"OAST", outside the exception statuses and the small codes programs exit
with; a parent with any other code, such as one that exits 1 by itself as
the stop comes, or one that ended before the stop was asked for, is not
stopped), the job's
`PeakProcessMemoryUsed` and `PeakJobMemoryUsed`, the per-process limit,
and whether a limit refused an allocation. `Start` associates a completion
port with the job before the child is created; a
`JOB_OBJECT_MSG_PROCESS_MEMORY_LIMIT` or `JOB_OBJECT_MSG_JOB_MEMORY_LIMIT`
on it marks the limit hit (evidence `job message`), and each
`JOB_OBJECT_MSG_ABNORMAL_EXIT_PROCESS` names a member that ended
abnormally. When no port can be associated, the limit counts as hit when
the peak came within 1/64 of it (evidence `peak`). Every `Wait` records
exactly one exit: when waiting on the parent or reading its exit code
fails, the record carries why in `Exit.Unknown` and no code. The child
keeps the error mode of the process that starts it.

**[FAC-P3] A contained start.** With `Options.Container` naming one
AppContainer's SID (`S-1-15-2-` and seven numbers), the child starts inside
that AppContainer with one capability, `ContainerCapabilitySID`, derived from
the name `openabstractions.sandbox`, through
`PROC_THREAD_ATTRIBUTE_SECURITY_CAPABILITIES` in the same `CreateProcess`
that assigns the job, so no instruction of the child runs uncontained. Its
descendants inherit the container. `Start` MUST refuse with `ErrContainer`,
before it creates a job or a process, a SID that is not one AppContainer's
(the all-application-packages groups and capability SIDs included) and an
environment that does not name `LOCALAPPDATA`, which Windows requires of a
contained start. A contained `CreateProcess` that fails is reported with
`ErrContainer`; `Start` never starts the child uncontained instead. The
profile, its file grants and what the container may reach are the caller's
(provider kit KIT-S1..KIT-S7). The capability is OA's own name: Windows
attaches no network, device or broker access to it, so the container reaches
through it only what an access list names it for. It is a TEMPORARY
EXCEPTION: the per-machine installer names it on the system drive's root and
the profiles folder (KIT-S7), and both go once the application that needs
them resolves its folders natively. Tests: `TestContainedStart`,
`TestContainerRefusals`, `TestContainerCapabilitySID`,
`TestContainedTokenCarriesOneCapability`.

**[FAC-P4] A handle lent to a running child.** `Process.Lend(h, access)`
duplicates a handle of the starting process into the original child;
`Process.LendTo(h, access, pid)` also supports its descendant, such as the
interpreter a venv launcher starts. The PID selects one process: under the
owned job's lifetime lock, the same opened process handle must prove live,
membership in that job and the exact AppContainer token SID before it receives
the duplicate. The copy has exactly `access` and ends with the target at the
latest. An external, wrong-container or ended target is refused. A provider
gives a contained engine a file it may not open by path this way: the engine
reads the bytes through the handle and cannot write them (`TestContainedStart`).

## Protected operations

A protected operation is one act the operating system allows only an
administrator, performed for the person after their yes in OA's prompt and the
platform's own administrator prompt (VISION 2026-10-02). `go-core/protected`
owns the helper's protocol, the runtime's ticket lifecycle and the launch.
The owning capability owns the act's request, execution and result checks;
the first and only act is one bounded page of the Windows Security log
(abstraction-logging LOG-N1). The design is
`research/release-0.4.0/planned/protected-operation-helpers.md`.

**[FAC-H1] The helper.** The helper is `openabstractions-system.exe`, beside
the runtime in the machine-wide installation's `tools` folder, and nowhere
else. Its command line MUST be exactly `<operation> --ticket <ticket>`, the
operation one of its compiled catalogue (`security-log-page`) and the ticket
1..128 bytes of lowercase hex; any other form MUST be refused before anything
else runs. Its first statement MUST restrict library loading to the system
directory (`SetDefaultDllDirectories(LOAD_LIBRARY_SEARCH_SYSTEM32)`), before
it parses its command line. It MUST refuse to act unless its own token is
elevated and its own image passes identity ID-D2's executable-folder mode
(`CheckExecutable`) with `MachineOwners()`: no other account may add a file
beside it. It connects to the runtime
only after identity ID-H2 accepts the server. It makes one claim exchange,
which returns the frozen request, performs the act under a 30-second deadline,
and makes one completion exchange carrying the result or the failure. It
validates the request against the operation's closed grammar itself, starts any
program by an absolute system-directory path with a fixed environment
(`SystemRoot` and `windir` from the Windows directory, `PATH` the system
directory alone, `LC_ALL=C`, never its own environment), never through `PATH`
or a shell, writes nothing to disk and keeps no state. An act is
machine-scoped: the elevated token may belong to another account, so no act
touches per-user state. Exit status: 0 completed, 2 refused command line,
3 refused by the runtime, 4 the act failed, 5 not elevated (or not on
Windows), 6 not the machine-wide installation's copy; a windowless elevated
image shows nothing else. Tests:
`TestHelperCommandLine`, `TestHelperRefusesWithoutElevation`,
`TestLibrarySearchIsRestrictedFirst`, `TestLibrarySearchSkipsAPlantedLibrary`
(serve/system), `TestTheExecutableFolderRule` (identity),
`TestProtectedRunnerEnvironment` (logging).

**[FAC-H2] The runtime's broker.** On Windows the runtime listens for the
helper at the account's endpoint `system-access-v1` (bootstrap.Endpoint) only
when it runs on its default endpoints and the helper beside its own image
passes ID-D2; otherwise protected operations are unavailable and no question is
admitted for them. A connection MUST be admitted only from a peer that
identity ID-H1 accepts for that helper path. A ticket is the ID of the system-
access question the runtime admitted for one exact request (abstraction-asks
ASK-S1). Its states are asked, claimed, fetched and delivered, and it ends
expired or failed; each transition happens once. A claim MUST name the ticket's
own operation and succeeds only while the runtime's reading of the question
shows `once` recorded by the operator within the last 120 seconds; the reply is
the request frozen at admission. A completion MUST come from the process that
claimed. A fetched result is held in memory for at most 120 seconds and is taken
once, by the owning service, for the same program and request: a timer drops
it and clears its bytes when the window ends, and every entry point sweeps
expired tickets as well, so it never outlives the window when nothing else
calls. At most 16 tickets are open. The runtime MUST check that room before it
admits a system-access question, and MUST withdraw a question whose ticket it
then cannot register, so no question waits on a ticket that does not exist. A
restart drops every ticket and result, and nothing protected is written to
disk; the restarted runtime withdraws the previous runtime's pending
system-access questions before it serves (abstraction-asks ASK-S1). Tests:
`TestBrokerLifecycle`, `TestBrokerRefusals` (go-core/protected),
`TestProtectedReadAtCapacityLeavesNoQuestion`,
`TestProtectedQuestionDoesNotOutliveItsRuntime` (serve).

**[FAC-H3] The launch.** Only the operator presenter launches the helper, and
only right after it recorded the person's `once` on a system-access question.
It MUST launch the exact installed helper path, which it checks with ID-D2's
executable-folder mode first, through `ShellExecuteExW` with the verb `runas`
and the answered prompt's owner window as owner, so the platform's prompt names
that program and opens in front of it; it never elevates a shell, a script host
or `openabstractions.exe`. `ERROR_CANCELLED` is the person's no: no helper runs
and the ticket expires unclaimed. The presenter records the machine's UAC
policy values and the helper's exit with the outcome. The Panel's Questions
page and the `openabstractions questions answer` command refuse `once` on a
system-access question and leave it to the native prompt. Tests:
`TestLaunchArguments` (go-core/protected), `TestSystemAccessLaunchFollowsOnce`
(monitor), `TestQuestionsSystemAccessOnceStaysWithThePrompt` (serve).

**[FAC-H4] What a protected operation does not cover.** The helper runs as
whoever the platform elevated. A standard account approved with an
administrator's password, and Windows 11 Administrator protection's separate
account, give the helper another account's endpoint name and a pipe it cannot
open, so the act fails closed; the owner accepted this for 0.4.0. macOS and
Linux have no helper in 0.4.0 and report the operation unavailable. A per-user
or development layout has no trusted helper path and reports it unavailable. A
hostile program running as the person can drive the runtime and so the
catalogue; the fixed operations, their bounded requests and the platform
prompt limit what it reaches (VISION 2026-10-06). The sandbox root entries
(FAC-P3) stay an installer action decided at install time.

## Outcomes

| service | call | outcomes |
|---|---|---|
| `endpoint@1` | `Describe` | `DescriptionOutcome`: `forbidden`, `unavailable`, `invalid` |
| `registry@1`, `registry@2` | `Declarations`, `Observe` | `DeclarationListOutcome`: `forbidden`, `unavailable`, `invalid` |
| `registry@1`, `registry@2` | `Declare`, `Withdraw` | `DeclarationEditOutcome`: `forbidden`, `unavailable`, `invalid`, `conflict` (a stale revision, name or endpoint); `Withdraw` also `unknown` |

A reading's `why` carries the typed cause as `<source>:<detail>` from
`program`, `describe`, `contract`, `remote`, `host`, `launch` and `exited`,
or the word `operator` for a disabled registration; no cause enum is
declared.

## Bounds

At most 64 registrations of every role and source together, 16 services and
guarantees, 64 arguments, resources and supported extension entries each; a
listing fits one 1 MiB frame. Protected operations: at most 16 open tickets, a
claim within 120 seconds of the person's `once`, a fetched result held at most
120 seconds, a helper deadline of 30 seconds and frames of at most 1 MiB
(FAC-H1, FAC-H2).
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
