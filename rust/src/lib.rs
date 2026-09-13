//! Transport-independent validated resolution and fixed service bindings.
pub use abstraction_frame::{FrameTransport, ScopedTransport};
use std::time::{Duration, Instant};
#[path = "../../rs/abstraction/facade/rec.rs"]
pub mod wire;
use wire::Resolver;

/// Trusted connector: enforce deadline, cancellation, frame bounds and peer identity.
/// supports must refuse transports/scopes whose guarantees it cannot preserve.
pub trait Connector: Clone {
    type Transport: FrameTransport + Clone;
    type Cancellation: Clone;
    fn supports(&self, scope: &str, transport: &str) -> bool;
    fn connect(
        &self,
        endpoint: &str,
        deadline: Instant,
        cancellation: Option<Self::Cancellation>,
        max_frame: usize,
    ) -> Result<Self::Transport, <Self::Transport as FrameTransport>::Error>;
}
pub type TransportError<C> = <<C as Connector>::Transport as FrameTransport>::Error;
#[derive(Debug)]
pub enum Error<E> {
    Transport(E),
    Call(wire::CallError<E>),
    InvalidRequirements,
    InvalidResolution,
    ResolutionStatus(String),
    UnsupportedTransport,
}
impl<E: std::fmt::Debug> std::fmt::Display for Error<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}
impl<E: std::fmt::Debug> std::error::Error for Error<E> {}
#[derive(Clone)]
pub struct Machine<C: Connector> {
    endpoint: String,
    connector: C,
    deadline: Option<Instant>,
    cancellation: Option<C::Cancellation>,
}
impl<C: Connector + Default> Machine<C> {
    pub fn new(endpoint: impl Into<String>) -> Self {
        Self::with_connector(endpoint, C::default())
    }
}
impl<C: Connector> Machine<C> {
    pub fn with_connector(endpoint: impl Into<String>, connector: C) -> Self {
        Self {
            endpoint: endpoint.into(),
            connector,
            deadline: None,
            cancellation: None,
        }
    }
    pub fn with_deadline(mut self, deadline: Instant) -> Self {
        self.deadline = Some(deadline);
        self
    }
    pub fn with_cancellation(mut self, cancellation: C::Cancellation) -> Self {
        self.cancellation = Some(cancellation);
        self
    }
    pub fn resolve_service(
        &self,
        contract: &str,
        guarantees: Vec<String>,
        scope: &str,
    ) -> Result<Binding<C>, Error<TransportError<C>>> {
        if !matches!(scope, "any" | "local" | "remote")
            || !distinct(&guarantees)
            || contract.is_empty()
        {
            return Err(Error::InvalidRequirements);
        }
        let deadline = self
            .deadline
            .unwrap_or_else(|| Instant::now() + Duration::from_secs(5));
        let request = wire::ResolveRequest {
            capability: contract.split('/').next().unwrap_or("").into(),
            contracts: vec![contract.into()],
            guarantees,
            scope: scope.into(),
        };
        let t = self
            .connector
            .connect(
                &self.endpoint,
                deadline,
                self.cancellation.clone(),
                1024 * 1024,
            )
            .map_err(Error::Transport)?;
        let result = wire::ResolverClient::new(t)
            .Resolve(wire::ResolveRequest {
                capability: request.capability.clone(),
                contracts: request.contracts.clone(),
                guarantees: request.guarantees.clone(),
                scope: request.scope.clone(),
            })
            .map_err(Error::Call)?;
        let reference = validate_binding(&request, result)?;
        if !self
            .connector
            .supports(&reference.scope, &reference.transport)
        {
            return Err(Error::UnsupportedTransport);
        }
        let mut binding = Binding::restore(
            self.connector.clone(),
            &reference.endpoint,
            self.deadline,
            self.cancellation.clone(),
            1024 * 1024,
        )
        .map_err(Error::Transport)?;
        binding.reference = Some(std::sync::Arc::new(reference));
        Ok(binding)
    }
}
/// Retains connection selection and reconstruction independently of capability semantics.
#[derive(Clone)]
pub struct Binding<C: Connector> {
    connector: C,
    endpoint: String,
    reference: Option<std::sync::Arc<wire::ServiceReference>>,
    transport: C::Transport,
    max_frame: usize,
    deadline: Option<Instant>,
    cancellation: Option<C::Cancellation>,
}
impl<C: Connector> Binding<C> {
    /// Caller-retained endpoint, using the explicitly supplied trusted connector.
    pub fn restore(
        connector: C,
        endpoint: &str,
        deadline: Option<Instant>,
        cancellation: Option<C::Cancellation>,
        max_frame: usize,
    ) -> Result<Self, TransportError<C>> {
        let transport = connector.connect(
            endpoint,
            call_deadline(deadline),
            cancellation.clone(),
            max_frame,
        )?;
        Ok(Self {
            connector,
            endpoint: endpoint.into(),
            reference: None,
            transport,
            max_frame,
            deadline,
            cancellation,
        })
    }
    pub fn endpoint(&self) -> &str {
        &self.endpoint
    }
    pub fn reference(&self) -> Option<&wire::ServiceReference> {
        self.reference.as_deref()
    }
    /// Explicit fresh waiting policy; endpoint and selected reference remain fixed.
    pub fn with_waiting(
        &self,
        deadline: Option<Instant>,
        cancellation: Option<C::Cancellation>,
    ) -> Result<Self, TransportError<C>> {
        let mut result = Self::restore(
            self.connector.clone(),
            &self.endpoint,
            deadline,
            cancellation,
            self.max_frame,
        )?;
        result.reference = self.reference.clone();
        Ok(result)
    }
    pub fn with_limit(mut self, max_frame: usize) -> Result<Self, TransportError<C>> {
        self.transport = self.connector.connect(
            &self.endpoint,
            call_deadline(self.deadline),
            self.cancellation.clone(),
            max_frame,
        )?;
        self.max_frame = max_frame;
        Ok(self)
    }
}
impl<C: Connector> FrameTransport for Binding<C> {
    type Error = TransportError<C>;
    fn write_frame(&self, b: &[u8]) -> Result<(), Self::Error> {
        if self.deadline.is_some() {
            self.transport.write_frame(b)
        } else {
            self.connector
                .connect(
                    &self.endpoint,
                    call_deadline(None),
                    self.cancellation.clone(),
                    self.max_frame,
                )?
                .write_frame(b)
        }
    }
    fn exchange_frame(&self, b: &[u8]) -> Result<Vec<u8>, Self::Error> {
        if self.deadline.is_some() {
            self.transport.exchange_frame(b)
        } else {
            self.connector
                .connect(
                    &self.endpoint,
                    call_deadline(None),
                    self.cancellation.clone(),
                    self.max_frame,
                )?
                .exchange_frame(b)
        }
    }
}
fn call_deadline(deadline: Option<Instant>) -> Instant {
    deadline.unwrap_or_else(|| Instant::now() + Duration::from_secs(5))
}
impl<C: Connector> ScopedTransport for Binding<C> {
    type Scoped = Self;
    fn call_scope(&self) -> Result<Self, Self::Error> {
        self.with_waiting(
            Some(call_deadline(self.deadline)),
            self.cancellation.clone(),
        )
    }
}
fn distinct(values: &[String]) -> bool {
    values
        .iter()
        .enumerate()
        .all(|(i, v)| !v.is_empty() && !values[..i].contains(v))
}
fn validate_binding<E>(
    request: &wire::ResolveRequest,
    result: wire::ResolveResult,
) -> Result<wire::ServiceReference, Error<E>> {
    if result.status != "resolved" {
        if result.reference.is_some() {
            return Err(Error::InvalidResolution);
        }
        return Err(Error::ResolutionStatus(result.status));
    }
    let r = result.reference.ok_or(Error::InvalidResolution)?;
    if r.provider.is_empty()
        || r.endpoint.is_empty()
        || r.endpoint.contains('\0')
        || r.capability != request.capability
        || !request.contracts.contains(&r.contract)
        || !matches!(r.scope.as_str(), "local" | "remote")
        || (request.scope != "any" && request.scope != r.scope)
        || !distinct(&r.guarantees)
        || !request.guarantees.iter().all(|g| r.guarantees.contains(g))
    {
        return Err(Error::InvalidResolution);
    }
    Ok(r)
}

#[cfg(test)]
mod tests;
