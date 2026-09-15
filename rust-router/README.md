# abstraction-facade-router

Resolves `abstraction.router/router@1` through a facade `Machine` and wraps the
generated client. `models` and `hosts` read inventory; `pick` chooses a
permitted host and refuses an empty model before any call. The router's policy
decides every call. `Error::service_code` returns `forbidden` for an evaluated
denial and `policy_unavailable` when the decision could not be obtained.

The crate depends on the pure resolver core and the generated
`abstraction-router-api` crate. Select `rust-native` separately for native
transport.
