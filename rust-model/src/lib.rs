//! Resolved model lookup returning portable download requests. `forbidden` is
//! an evaluated lookup refusal and `unavailable` means the decision or registry
//! could not answer; neither carries a request.
use abstraction_facade_service::{Binding, Connector, Machine, TransportError};
pub use abstraction_download_request_api as request;
pub use abstraction_model_api as wire;
use wire::ModelResolver as _;

#[derive(Debug)]
pub enum Error<E> {
    Call(wire::CallError<E>),
    Invalid(&'static str),
}
impl<E: std::fmt::Debug> std::fmt::Display for Error<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl<E: std::fmt::Debug> std::error::Error for Error<E> {}

fn require<E>(ok: bool, s: &'static str) -> Result<(), Error<E>> {
    if ok {
        Ok(())
    } else {
        Err(Error::Invalid(s))
    }
}

fn digest(s: &str) -> bool {
    s.len() == 71 && s.starts_with("sha256:") && s.as_bytes()[7..].iter().all(|c| c.is_ascii_hexdigit())
}

/// An anonymous HTTP(S) mapping: matching scheme, nonempty host, no user info.
fn source(s: &request::Source) -> bool {
    if s.scheme != "http" && s.scheme != "https" {
        return false;
    }
    let Some(rest) = s.locator.strip_prefix(&format!("{}://", s.scheme)) else {
        return false;
    };
    let authority = rest.split(['/', '?', '#']).next().unwrap_or("");
    !authority.is_empty() && !authority.contains('@') && !authority.starts_with(':')
}

/// Checks a lookup result against its contract before exposing a request:
/// only `resolved` carries one, with a SHA256 digest and anonymous HTTP(S) sources.
pub fn check_lookup<E>(r: &wire::LookupResult) -> Result<(), Error<E>> {
    require(
        matches!(r.outcome.as_str(), "resolved" | "invalid" | "unavailable" | "unsupported_mapping" | "forbidden"),
        "lookup outcome",
    )?;
    require((r.outcome == "resolved") == r.request.is_some(), "lookup request presence")?;
    if let Some(v) = &r.request {
        require(
            digest(&v.artifact.digest) && v.artifact.size >= 0 && !v.sources.is_empty() && v.sources.iter().all(source),
            "resolved request",
        )?;
    }
    Ok(())
}

/// One model resolver binding. Each call is one exchange with no retry.
#[derive(Clone)]
pub struct Model<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Model<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn resolve(&self, reference: wire::Ref) -> Result<wire::LookupResult, Error<T::Error>> {
        let r = wire::ModelResolverClient::new(self.transport.clone())
            .resolve(reference)
            .map_err(Error::Call)?;
        check_lookup::<T::Error>(&r)?;
        Ok(r)
    }
}

pub trait ModelMachine<C: Connector> {
    /// Resolution grants no lookup authority; the service's lookup policy decides each call.
    fn resolve_model(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Model<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
}
impl<C: Connector> ModelMachine<C> for Machine<C> {
    fn resolve_model(
        &self,
        g: Vec<String>,
        s: abstraction_facade_service::wire::Scope,
    ) -> Result<Model<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Model::new(self.resolve_service("abstraction.model/resolver@1", g, s)?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn resolved(locator: &str) -> wire::LookupResult {
        let mut v = request::Request::default();
        v.artifact.digest = format!("sha256:{}", "a".repeat(64));
        let s = request::Source { scheme: "https".into(), locator: locator.into(), credential: String::new() };
        v.sources = vec![s];
        wire::LookupResult { outcome: wire::LookupOutcome::Resolved, request: Some(v) }
    }
    #[test]
    fn lookup_results_keep_their_shapes() {
        assert!(check_lookup::<()>(&resolved("https://example.invalid/weights")).is_ok());
        assert!(check_lookup::<()>(&resolved("https://user@example.invalid/weights")).is_err());
        assert!(check_lookup::<()>(&resolved("http://example.invalid/weights")).is_err());
        assert!(check_lookup::<()>(&resolved("https:///weights")).is_err());
        let mut empty = resolved("https://example.invalid/weights");
        empty.request.as_mut().unwrap().sources.clear();
        assert!(check_lookup::<()>(&empty).is_err());
        use wire::LookupOutcome::{Forbidden, Invalid, Unavailable, UnsupportedMapping};
        for outcome in [Forbidden, Unavailable, Invalid, UnsupportedMapping] {
            let mut r = wire::LookupResult { outcome, request: None };
            assert!(check_lookup::<()>(&r).is_ok(), "{outcome}");
            r.request = resolved("https://example.invalid/w").request;
            assert!(check_lookup::<()>(&r).is_err(), "{outcome} with request");
        }
        let bare = wire::LookupResult { outcome: wire::LookupOutcome::Resolved, request: None };
        assert!(check_lookup::<()>(&bare).is_err());
    }
}
