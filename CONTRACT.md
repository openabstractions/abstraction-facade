# abstraction.facade contract: endpoint@1 and registry@1

[facade.thrift](facade.thrift) defines resolution ([RESOLUTION.md](RESOLUTION.md)),
the caller echo, and the two profiles this contract judges:
`abstraction.facade/endpoint@1`, which every generated endpoint serves, and
`abstraction.facade/registry@1`, the provider declarations that feed the
runtime's resolution catalogue (research/provider-registry/DECISION.md).

## The endpoint description

- **ENDPOINT-1.** Every Go and C++ dispatcher generated for a service with a
  request-response method answers `Describe` (idl/LANGUAGE.md DEF-S2). An
  endpoint hosting several services lists every one in its own order. A
  service is `ready` unless its handler's readiness hook reports otherwise,
  and then `not_ready` with the hook's reason in `why`.
- **ENDPOINT-2.** `program` and `version` are the provider's own words and
  never authority. The caller binds the server by its connection proof. A
  registry requires the declared program as the server before it reads a
  description.
- **ENDPOINT-3.** An endpoint built before this profile answers
  `unknown_service`. A frame for any other undeclared service still reads
  `unknown_service`.

## The provider registry

- **REG-1.** A declaration is `providers/<name>.json` in the runtime state,
  version 2, written by `Declare` and read at start. It keeps `resources`,
  `models` and `remote`. A version 1 file, which carried `stores` and `profiles`, is
  rewritten once at start: each store becomes `store:<name>` and each profile
  `profile:<name>`. Each `hosts.json` hosted entry of wire `oa-remote@1`
  becomes a remote declaration serving `abstraction.inference/chat@1` and
  `abstraction.router/router@1`, with its credential in `remote.credential`,
  and leaves `hosts.json`. Declarations are written before `hosts.json`, so an
  interrupted rewrite resumes on the next start.
- **REG-2.** The runtime binds a local provider by program path. Every probe
  calls endpoint@1 `Describe` over a connection requiring the declared program
  as the server. A process running another program reads `refused` with why
  `program:<detail>`. A failed Describe reads `unreachable` with why
  `describe:<code>`, `describe:unknown_service` for a provider built before
  endpoint@1. A declaration is `ready` when the description lists every
  declared contract ready. A contract absent or not ready reads `not_ready`
  with why `contract:<wire name>:<reason>`, and its candidates are withdrawn.
  An `on_demand` provider launches when a resolution it would satisfy finds it
  not ready, and relaunches after an exit with a backoff of 250 ms doubling to
  30 s, reset after a minute up. `attach` probes a provider something else
  runs. A native `abstraction.inference/chat@1` declaration with an explicit
  model allowlist is a mediated local resolver candidate after the runtime's own,
  with the declaration's name as provider, its guarantees plus the local-only
  guarantee, and an OA endpoint pinned to that declaration generation. Other declared contracts remain visible to operators and
  expose no application endpoint until their capability owns mediation.
  `Withdraw` removes the candidates and ends a launched child. A generation's
  OA endpoint stays reachable while chat operations selected through it remain
  active or retained under the inference contract; it refuses every new Start
  after withdrawal and closes when those operations retire. At most 64 active
  and retained generation endpoints exist, so a new candidate remains not ready
  until a retained endpoint closes. A program cannot declare itself.
- **REG-3.** Each capability reads its own acceptance rule for a declaration's
  resources. A `store:<name>` resource belongs to an inventory source, which
  names at least one. `Declare` writes the permit rule
  `abstraction.storage/inventory.provide` on `store:<name>` for the declared
  program with why `provider add`, leaving an existing rule as it is. At each
  probe the runtime accepts a store the source describes only when the
  declaration names it and the rule permits it, and lists it in `accepted`. An
  on-demand source launches when the runtime starts serving, because the
  runtime is its only reader. A native inference declaration's `models` are a
  trusted routing allowlist; chat@1 and endpoint@1 provide no model inventory.
  The list accepts common provider names such as `owner/model` and does not
  confer a rights grant.
- **REG-4.** A remote runtime is a declaration of transport `oa-remote@1`,
  activation `remote`, endpoint `tls://<host>:<port>`, no program, and
  `remote` naming the server name, the PEM files of the trusted roots and this
  runtime's client certificate and key, and the credential the remote holds.
  `Declare` refuses trust files it cannot load as `invalid` with reason
  `remote`, and writes `abstraction.inference/complete` on `host:<name>` for
  the runtime's operator programs and the caller. Its readiness is its
  endpoint@1 Describe over mutual TLS. The router reads the remote's router@1
  `Models` for the host's listing and its `Hosts` as `<name>/<host>` with
  `domain` `<name>`, and a chat@1 operation picked for it is delegated as the
  inference CONTRACT's "Remote runtimes" section states. No registry is read
  across machines.
- **REG-5.** `registry_actions` is closed: `abstraction.facade/provider.manage`
  on resource `account` decides every registry call for the bound caller. The
  default installation grants it to the runtime's operator programs. A caller
  without it reads `forbidden`, and a decision point that cannot answer reads
  `unavailable`. Edits are conditional on the revision, the digest of every
  declaration file. `Observe` answers when the digest of the declarations and
  their readings differs from the cursor, or when `wait_ms` (0..30000) ends.

## Rule answers

1. **Outcomes.** `Describe` returns `DescriptionOutcome`, `Declarations` and
   `Observe` `DeclarationListOutcome`, and `Declare` and `Withdraw`
   `DeclarationEditOutcome`. All reserve `forbidden`, `unavailable` and
   `invalid`. The edits add `conflict` for a stale revision, name or endpoint,
   and `Withdraw` adds `unknown`.
2. **Retained records.** A declaration is identified by its name and kept until
   withdrawn. A reading is computed by the runtime and never stored.
3. **Failures.** A reading's `why` carries the typed cause as
   `<source>:<detail>` from `program`, `describe`, `contract`, `remote`,
   `launch` and `exited`. No cause enum is declared.
4. **Catalogues.** `DeclarationTransport` is the closed provider-declaration
   transport choice. `declaration_resource_kinds` grows with the runtime that
   reads it. `registry_actions` is closed by REG-5. `capabilities` in a
   description is an open map of display facts.
5. **Identity fields.** `declared_by` is asserted by the runtime from the bound
   caller. A description's `program` is the provider's own claim (ENDPOINT-2).
6. **Bounds.** At most 64 declarations, 16 contracts and guarantees, 64
   arguments and resources each; a listing fits one 1 MiB frame.
7. **Unknown members.** Every enum is acted on and refuses unknown members.
8. **Entry points.** ENDPOINT rules judge every generated endpoint; REG rules
   judge the runtime's registry.
9. **Duplicate keys.** Refused, as for the whole definition.
10. **First definition.** Recorded in `docs/BASE-PROTOCOL-CHANGES.md`.
11. **Reserved words.** No field, method or parameter name maps to a reserved
    word in Go, C++, Python, Rust or JavaScript.
