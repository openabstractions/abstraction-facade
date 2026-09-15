# Resolved configuration editor

This optional crate adds `ConfigMachine` to the pure facade `Machine<C>`.
`resolve_config_editor(vec![], "local")` returns a fixed `Editor<Binding<C>>`
bound to `abstraction.config/editor@1`. Select installed native IPC through the
separate facade-native crate, or supply another trusted Connector.

`read_user` returns the service user's rung and its revision. `replace_user`
compares the expected revision and returns the generated outcome: `applied` or
`conflict` with a snapshot, `forbidden` for an evaluated edit-policy refusal, or
`unavailable` when the policy decision could not be obtained. Both refusals carry
empty values and an empty revision and change nothing; `unavailable` may be
retried. An empty expected revision is refused before any exchange. There is no
automatic retry; reread to reconcile an uncertain replacement.

The crate depends only on the facade core and the generated config protocol,
which depends on the pure shared frame contract. Source version 0.0.0 is current
development packaging, not a registry release.
