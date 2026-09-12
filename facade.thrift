namespace * abstraction.facade

encoding json {
 escape="minimal"
 indent="2"
 map_keys="utf8-bytes"
 numbers="integer-decimal"
 opaque="verbatim"
 terminator="newline"
 duplicate_keys="refuse"
 depth_limit="64"
}
refusal {
 1: malformed(stage="grammar")
 2: bad_string(stage="grammar")
 3: number_spelling(stage="grammar")
 4: wrong_type(stage="grammar")
 5: depth_exceeded(stage="grammar")
 6: duplicate_key(stage="grammar")
 7: duplicate_field(stage="structure")
 8: unknown_field(stage="structure")
 9: missing_field(stage="structure")
 10: bad_enum(stage="structure")
 11: trailing_bytes(stage="document")
}

enum Scope { 1: any 2: local 3: remote }(unknown="refuse")
enum ResolutionStatus {
 1: resolved
 2: unavailable
 3: forbidden
 4: incompatible
 5: unmet_requirements
 6: not_ready
 7: invalid_request
}(unknown="refuse")
struct ResolveRequest {
 1: required string capability
 2: required list<string> contracts
 3: required list<string> guarantees
 4: required Scope scope
}(unknown_fields="refuse",doc="Capability and acceptable contract identities, required guarantees and permitted placement. Contains no caller identity or provider preference. Contract list must be nonempty; guarantees may be empty.")
struct ServiceReference {
 1: required string provider
 2: required string capability
 3: required string contract
 4: required list<string> guarantees
 5: required Scope scope
 6: required string transport
 7: required string endpoint
}(unknown_fields="refuse",doc="A candidate service binding, not acceptance or authority. Provider identifies a logical provider, not a PID. Scope is local or remote. Contract is an exact versioned service identity; endpoint is opaque to application code.")
struct ResolveResult {
 1: required ResolutionStatus status
 2: optional ServiceReference reference(omit="absent")
}(document="true",unknown_fields="refuse",doc="Reference is present exactly when resolved. Failure status describes the first unsatisfied resolution stage without exposing disallowed provider metadata. Service authentication and acceptance remain necessary after resolution.")
service Resolver {
 ResolveResult Resolve(1:ResolveRequest request)(doc="Find an authorized ready candidate satisfying an exact acceptable contract and all required guarantees. Does not submit work, activate an embedded provider, or transfer ownership.")
}(wire_name="abstraction.facade/resolver@1",doc="Common provider resolution vocabulary. Authorization comes from the receiving boundary, never from request fields. See RESOLUTION.md for selection and refusal semantics.")
