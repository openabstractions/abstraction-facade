//! Resolved router inventory and host picks. Policy refusals arrive as service
//! error codes: `forbidden` for an evaluated denial and `policy_unavailable`
//! when the decision could not be obtained.
use abstraction_facade_service::{Binding, Connector, Machine, TransportError};
pub use abstraction_router_api as wire;
use wire::Router as _;

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
impl<E> Error<E> {
    /// The receiving service's error code, when the call reached the router.
    pub fn service_code(&self) -> Option<&str> {
        match self {
            Error::Call(wire::CallError::Service(e)) => Some(&e.code),
            _ => None,
        }
    }
}

fn require<E>(ok: bool, s: &'static str) -> Result<(), Error<E>> {
    if ok {
        Ok(())
    } else {
        Err(Error::Invalid(s))
    }
}

/// One router binding. Each call is one exchange with no retry.
#[derive(Clone)]
pub struct Router<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Router<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    fn client(&self) -> wire::RouterClient<T> {
        wire::RouterClient::new(self.transport.clone())
    }
    pub fn models(&self, fresh: bool) -> Result<wire::ModelsSnapshot, Error<T::Error>> {
        self.client().Models(fresh).map_err(Error::Call)
    }
    pub fn hosts(&self, fresh: bool) -> Result<wire::HostsSnapshot, Error<T::Error>> {
        self.client().Hosts(fresh).map_err(Error::Call)
    }
    /// Chooses a permitted host without loading a model. Absent allowance
    /// permits all servable hosts; an empty allowance permits none.
    pub fn pick(&self, request: wire::PickRequest) -> Result<wire::PickResult, Error<T::Error>> {
        require(!request.model.is_empty(), "model required")?;
        let asked = request.model.clone();
        let r = self.client().Pick(request).map_err(Error::Call)?;
        require(r.decision.asked == asked, "decision for another model")?;
        Ok(r)
    }
}

pub trait RouterMachine<C: Connector> {
    /// Resolution grants no routing authority; the router's policy decides each call.
    fn resolve_router(
        &self,
        guarantees: Vec<String>,
        scope: &str,
    ) -> Result<Router<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
}
impl<C: Connector> RouterMachine<C> for Machine<C> {
    fn resolve_router(
        &self,
        g: Vec<String>,
        s: &str,
    ) -> Result<Router<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Router::new(self.resolve_service("abstraction.router/router@1", g, s)?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn service_codes_are_exposed_only_for_service_errors() {
        let refused: Error<()> = Error::Call(wire::CallError::Service(wire::ServiceError {
            code: "policy_unavailable".into(),
            message: "decision lookup".into(),
        }));
        assert_eq!(refused.service_code(), Some("policy_unavailable"));
        assert_eq!(Error::<()>::Call(wire::CallError::Transport(())).service_code(), None);
        assert_eq!(Error::<()>::Invalid("model required").service_code(), None);
    }
}
