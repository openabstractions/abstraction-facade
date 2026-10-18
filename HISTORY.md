# History: abstraction-facade

Linked from [CONTRACT.md](CONTRACT.md)'s "Reading this page."

## Superseded ids

- **`ENDPOINT-1`..`ENDPOINT-3`** and the dated **`FAC-R (2026-09-22)`**
  became `FAC-B1`..`FAC-B4` (binding): the resolved endpoint is the binding
  (`reference.html:49`).
- **`REG-1`..`REG-8`** became `FAC-R1`..`FAC-R8` (registry). `REG-8`, which
  was defined mid-paragraph inside `REG-2`'s text, is now its own rule,
  `FAC-R8`, positioned after `FAC-R7`.

  Per `research/vocabulary/RENAME-PLAN.md`'s facade step 1 and
  `research/vocabulary/DECISION.md` S3/S4/S12. `ENDPOINT`, dated `FAC-R` and
  `REG` are retired and never reused. Ids carry no dates from this release
  on.

  The dated `FAC-R (2026-09-22)` became [FAC-B4], the resolved reference.

| retired | now | rule |
| --- | --- | --- |
| `ENDPOINT-1` | [FAC-B1] | a generated dispatcher describes its service |
| `ENDPOINT-2` | [FAC-B2] | `program` and `version` are the provider's own words |
| `ENDPOINT-3` | [FAC-B3] | an endpoint built before the service answers unknown |
| `REG-1` | [FAC-R1] | registration file |
| `REG-2` | [FAC-R2] | local providers |
| `REG-3` | [FAC-R3] | resource acceptance |
| `REG-4` | [FAC-R4] | remote runtimes |
| `REG-5` | [FAC-R5] | registry admission |
| `REG-6` | [FAC-R6] | roles |
| `REG-7` | [FAC-R7] | reading registrations |
| `REG-8` | [FAC-R8] | activation |

### Inherited handles, 2026-10-01

A provider host lends a model's memory section to the engine it launches by
handle inheritance (VISION 2026-10-01;
`research/model-locators/MEMORY-LENDING.md` §3, §9 step 2). The research
prototype passed the handle through `os/exec` and assigned the job after the
child started; `supervision` now carries the handle and keeps the atomic job
assignment.

| id | before | now |
| --- | --- | --- |
| FAC-P1 | none; a supervised child inherited its three standard handles only | binding: `Options.Inherit` names further inheritable handles, joined to the one handle list; anything else is refused with `ErrInherit` before a job or process exists |
| FAC-P2 | none; `Wait` returned the parent's exit code alone | binding: `Process.Exit` reports the code and its exception status, run time, stop, the job's memory peaks and limit, a limit refusal from the job's completion port (or inferred from the peak) and abnormal member exits; modelhost logs it per engine (MODELHOST-E1) after a PyTorch engine ended with no trace (`research/live-runs/2026-10-02.md`, torch-live) |

### A declared card's wake lease, 2026-10-02

A video provider declared with `provider add` could name no card, so its
program got neither `hold` on `card:0` nor on `awake`, and modelhost's GPU
fault guard was refused the wake lease at every video start
(`research/live-runs/2026-10-02.md`, phase 2 finding 3). The registry already
wrote both rules for a declared card; the rule text named only the card.

| id | before | now |
| --- | --- | --- |
| FAC-R3 | `card:<n>` takes `abstraction.resource/hold` on the card | the card and `awake`; `provider add --card <n>` names one |

### Bundles and paired outputs, 2026-10-02

An application's first video job through the runtime needed six rules, each
learnt from a separate refusal (`research/live-runs/2026-10-02.md`, phase 2
finding 4). The coordinator decided: a video output profile, one combined
refusal that admits every first-use question at once, output pairing that
reaches only the program's own results, and an inference bundle that adds
job submission and the output pairs it names. `go/grants` gained
`PairedOutput` and `Paired` (an answer on `content.write` for
`output:image` or `output:video` also writes `content.read`), and the
`inference` bundle gained `acceptance.submit` and `For.Outputs`. No rule id
of this contract changed; inference INF-S7 and INF-V4 state the behaviour.

The review of that change found the inference bundle's `acceptance.submit`
on the acceptance contract let the program queue URL downloads too.
Submission is now decided per kind (job JOB-A13): `runtime.JobPolicyFromRights`
decides the acceptance contract first and, without a rule there, the
submission's `job:<kind>` (`JobKindResource`); the inference bundle writes
`job:inference` and the downloads bundle `job:download`.

An explicit deny now wins (`research/wake-and-inference-rights/REPORT.md`
recommendation 6): under a permit on the acceptance contract,
`JobPolicyFromRights` still reads the kind's rule, through
`runtime.QuietDecider` where the host has one so the read asks nothing, and a
deny there refuses that kind.

`grants.OutputProfileResources` gained `output:speech`
(`research/wake-and-inference-rights/REPORT.md` recommendation 7): one
answer on `content.write` for speech output also writes `content.read`, as
for image and video. The runtime now asks it for a synchronous `speech@1` or
`image@1` call as well as for a job. `grants.OutputProfiles` gained
`speech` too, so `rights grant --for inference --output speech` writes the
same pair, whose read reaches only the program's own results.

### A launched provider learns the runtime's endpoints, 2026-10-02

The live walkthrough of 2026-10-02 (D8) declared a model host's lease
endpoint as `<runtime endpoint>-resource-table`, which an isolated runtime
does not serve, and the application saw only `engine:unavailable`.
Coordinator's decision: the runtime names its own services to the children
it launches, and a declaration carries an endpoint only as an override.
serve's `providerEnvironment`; `openabstractions models host` declares no
`--resource-endpoint` unless given one and drops one an earlier version
derived.

| id | before | now |
| --- | --- | --- |
| FAC-R8 | a launched child gets the runtime's environment unchanged | it also gets the resource, log, config and router endpoints, and the runtime endpoint when not the installed one, in the variables their clients read; a declared argument overrides |
| FAC-R8 | (same day, review) an endpoint variable the runtime does not set was inherited from the runtime's parent | the child gets none of the five the runtime does not set: no `ABSTRACTION_RUNTIME_ENDPOINT` from an installed runtime, none of the five under XPC; every other variable passes through |

## Reviewed and applied

- `research/reviews/inference-facade-2026-09-23.md` F9 — `Scope` is
  placement. `facade.thrift`'s `Scope`/`Requirements.Scope` stays on the
  wire until `resolver@2` (S13's prose-to-wire table), but FAC-R7's "in
  either scope" now reads "in a per-user or machine-wide install" (D19); the
  bare word "scope" does not otherwise appear in this contract's own prose,
  the field-name citations in FAC-B4 excepted.
- `research/reviews/inference-facade-2026-09-23.md` F10 — rule shape.
  `FAC-R8` (was `REG-8`) is now its own rule after `FAC-R7` instead of
  embedded in `REG-2`'s paragraph. `FAC-R3` (was `REG-3`) states the
  `provider add` permit-rule write once instead of twice. `FAC-R2` (was
  `REG-2`) drops "The runtime binds a local provider by program path" and
  opens with "Every probe calls `endpoint@1` `Describe` over a connection
  requiring the registered program as the server," removing identity's
  `Bind` from a contract whose own `Binding` names the resolved client.
- `research/reviews/router-2026-09-23.md` F9 — checked, not changed. F9
  compares `ROUTE-2` against `INF-R1` and `REG-5` (now `FAC-R5`), which
  already name `abstraction.facade/provider.manage` on resource `account`
  for every registry call; no rule here needed the finding's fix.

## Applied from research/vocabulary/DECISION.md

- D4: `contract` (the `abstraction.x/y@n` string) is `service` in prose;
  the field name `contract` and the literal reason prefix `contract:` stay
  on the wire until `resolver@2` (S13 table). D5's separate "contract" sense
  — this page, and a capability's own rule set — is unchanged and still
  called `contract`.
- D10: registry role values keep their wire spelling; prose now says a
  managed process (`provider`), a model server (`host`), a remote runtime
  (`remote`) — FAC-R6.
- D11/D83: the bare noun "host" is "a model server" or "server" in prose
  throughout the registry rules; the resource string `host:<name>` and the
  field `host.ceiling` stay on the wire, owned by rights, lend and
  inference's own future steps, not this one.
- D15: `Requirements.Placement`/`PlacementLocal` at `resolver@2`; this
  contract's own prose used the word "scope" only in FAC-R7's install-scope
  sense (D19), now "a per-user or machine-wide install".
- D19: "in either scope" (FAC-R7) is "in a per-user or machine-wide
  install".
- D41: the opening paragraph's "the one directory of the programs a runtime
  knows" is now "the one registry of the programs a runtime knows". The
  filesystem folder FAC-R7 reads registration files from is still called a
  directory — a different, ordinary sense of the word, not the registry
  concept D41 renames.
- D42: "declaration" is "registration" and "declare"/"declared" (the verb,
  where it names the act of making a provider known) is "register"/
  "registered" throughout FAC-R1–FAC-R8 and FAC-R2's closing sentence, "A
  program cannot register itself." The wire method `Declare` and the field
  and type names built on `Declaration*` stay until `registry@2` (S13
  table); "declared in facade.thrift", describing where an interface is
  defined, is not this sense and is untouched. "Operator siblings" (FAC-R7,
  twice) is now "the installation's other programs".
- D55: "declaration generation" is "registration's revision" (FAC-R8); "a
  generation's OA endpoint" is "a revision's OA endpoint"; "retained
  generation endpoints" is "retained revision endpoints". The unrelated,
  ordinary-English "before generation" in README.md's caller-echo section
  (code generation) is untouched.

## Dropped as generic boilerplate

The eleven-item "Rule answers" checklist (Outcomes, Retained records,
Failures, Catalogues, Identity fields, Bounds, Unknown members, Entry
points, Duplicate keys, First definition, Reserved words) that closed this
page is gone, matching `abstraction-resource`, `abstraction-rights` and
`abstraction-provider-modelhost`'s own shape conversions. Outcomes and
Bounds became the page's own sections; Retained records and part of
Identity fields (`declared_by` for a `Declare`-written registration) moved
into FAC-R1; Failures' `why`-cause list moved to a paragraph under
Outcomes; Catalogues moved into Bounds; Entry points is now the letter
table; Unknown members, Duplicate keys, First definition and Reserved words
were project-wide invariants repeated on every contract page and are not
restated here.

## Not carried forward here

`RESOLUTION.md` cites this page's old `FAC-R` id ("CONTRACT.md FAC-R") and
uses "contract"/"scope" in the D4/D15 sense in several places; `APPLICATIONS.md`
uses "descriptor" where D69 says "manifest". Both files, and this module's
own generated-language source trees (`go/client`, `cpp`, `py`, `rust`,
`javascript`) and their test and comment citations of `ENDPOINT-*` and
`REG-*`, are outside this change's assignment (WRITES: `CONTRACT.md`, this
page, `README.md`) and are a follow-up.
