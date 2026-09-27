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
