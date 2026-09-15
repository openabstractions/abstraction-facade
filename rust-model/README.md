# abstraction-facade-model

Resolves `abstraction.model/resolver@1` through a facade `Machine` and wraps the
generated client. `resolve` returns the typed lookup result after checking its
contract: only `resolved` carries a portable download request, with a SHA256
digest and anonymous HTTP(S) sources. `forbidden` is an evaluated lookup
refusal and `unavailable` means the decision or registry could not answer.

The generated `abstraction-model-api` crate imports its request records from
`abstraction-download-request-api` through `--rust-import`, and this crate
re-exports both. Select `rust-native` separately for native transport.
