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
    fn supports(&self, scope: wire::Scope, transport: &str) -> bool;
    fn connect(
        &self,
        endpoint: &str,
        deadline: Instant,
        cancellation: Option<Self::Cancellation>,
        max_frame: usize,
    ) -> Result<Self::Transport, <Self::Transport as FrameTransport>::Error>;
    /// `connect` for the runtime's resolver endpoint, which serves sessions
    /// (FRAMING.md "Sessions"); a connector that can keep connections asks for
    /// them here. The default is `connect`.
    fn connect_resolver(
        &self,
        endpoint: &str,
        deadline: Instant,
        cancellation: Option<Self::Cancellation>,
        max_frame: usize,
    ) -> Result<Self::Transport, <Self::Transport as FrameTransport>::Error> {
        self.connect(endpoint, deadline, cancellation, max_frame)
    }
    /// The installed runtime's resolver endpoint, or why none can be selected.
    fn runtime_endpoint(&self) -> Result<String, <Self::Transport as FrameTransport>::Error>;
    /// Selects the installed runtime's identity for one resolution, within its
    /// deadline, and returns the connector that resolution uses for every
    /// connection: the resolver's and the selected provider's. A failure is that
    /// resolution's `runtime_unavailable`, and nothing is contacted. The default
    /// returns this connector unchanged, for a connector whose `connect` already
    /// establishes peer identity for every endpoint it accepts.
    fn select_installed(
        &self,
        _deadline: Instant,
        _cancellation: Option<Self::Cancellation>,
    ) -> Result<Self, <Self::Transport as FrameTransport>::Error> {
        Ok(self.clone())
    }
    /// Whether a transport error is the caller's own cancellation, which
    /// resolution returns unchanged instead of as a resolution error.
    fn is_cancellation(&self, _error: &<Self::Transport as FrameTransport>::Error) -> bool {
        false
    }
    /// The operating system the installed runtime would run on, as
    /// `std::env::consts::OS` names it.
    fn platform(&self) -> &str {
        std::env::consts::OS
    }
}

/// The platform the runtime's platform declaration lists as unsupported for an
/// operating system named as `std::env::consts::OS` names it: `android` or `macos`.
pub fn unsupported_platform(os: &str) -> Option<&'static str> {
    match os {
        "android" => Some("android"),
        "macos" => Some("macos"),
        _ => None,
    }
}
pub type TransportError<C> = <<C as Connector>::Transport as FrameTransport>::Error;

/// The client-side resolution statuses; a resolver refusal keeps its own word.
pub const RUNTIME_UNAVAILABLE: &str = "runtime_unavailable";
pub const INVALID_RESOLUTION: &str = "invalid_resolution";
pub const UNSUPPORTED_TRANSPORT: &str = "unsupported_transport";

/// Why a resolve call produced no usable service.
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum ResolutionFailure {
    /// No runtime could be selected or reached; the transport error is the source.
    RuntimeUnavailable,
    /// A reachable resolver refused, for example `unavailable` when it serves
    /// no registration for the contract.
    Refused(wire::ResolutionStatus),
    /// The resolver's answer failed validation.
    InvalidResolution,
    /// The resolver selected a reference this connector cannot use.
    UnsupportedTransport {
        scope: wire::Scope,
        transport: String,
    },
}
impl ResolutionFailure {
    /// The status word, as the other language bindings report it.
    pub fn status(&self) -> &'static str {
        match self {
            Self::RuntimeUnavailable => RUNTIME_UNAVAILABLE,
            Self::Refused(status) => status.as_str(),
            Self::InvalidResolution => INVALID_RESOLUTION,
            Self::UnsupportedTransport { .. } => UNSUPPORTED_TRANSPORT,
        }
    }
}

/// A resolve call that produced no usable service, with what it asked for and
/// what it looked for: `the installed runtime`, `the installed runtime at
/// <endpoint>` or `the explicit endpoint <endpoint>`.
#[derive(Debug)]
pub struct ResolutionError<E> {
    pub failure: ResolutionFailure,
    pub capability: String,
    pub contract: String,
    pub looked_for: String,
    /// The selection or transport failure, for `RuntimeUnavailable`.
    pub cause: Option<E>,
    /// The platform the runtime declares unsupported, for `RuntimeUnavailable`
    /// before any selection: `android` or `macos`.
    pub platform: Option<&'static str>,
}
impl<E> std::fmt::Display for ResolutionError<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "service resolution: {}: {} (capability {}) at {}",
            self.failure.status(),
            self.contract,
            self.capability,
            self.looked_for
        )?;
        if let ResolutionFailure::UnsupportedTransport { scope, transport } = &self.failure {
            write!(f, ": {scope} reference over {transport}")?;
        }
        if let Some(platform) = self.platform {
            write!(
                f,
                ": no supported OpenAbstractions runtime exists for {platform}"
            )?;
        }
        Ok(())
    }
}

#[derive(Debug)]
pub enum Error<E> {
    /// A provider connection failed, or the caller cancelled resolution.
    Transport(E),
    Call(wire::CallError<E>),
    InvalidRequirements,
    /// Resolution produced no usable service; `source()` is the transport error
    /// when no runtime could be selected or reached.
    Resolution(ResolutionError<E>),
}
impl<E: std::fmt::Debug> std::fmt::Display for Error<E> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Resolution(r) => write!(f, "{r}"),
            other => write!(f, "{other:?}"),
        }
    }
}
impl<E: std::error::Error + 'static> std::error::Error for Error<E> {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            Error::Transport(e) => Some(e),
            Error::Resolution(r) => r
                .cause
                .as_ref()
                .map(|e| e as &(dyn std::error::Error + 'static)),
            _ => None,
        }
    }
}
#[derive(Clone)]
pub struct Machine<C: Connector> {
    /// None selects the installed runtime through the connector at each resolution.
    endpoint: Option<String>,
    connector: C,
    deadline: Option<Instant>,
    cancellation: Option<C::Cancellation>,
}
impl<C: Connector + Default> Machine<C> {
    pub fn new(endpoint: impl Into<String>) -> Self {
        Self::with_connector(endpoint, C::default())
    }
    /// Resolves through the installed runtime the connector selects.
    pub fn installed() -> Self {
        Self::installed_with_connector(C::default())
    }
}
impl<C: Connector> Machine<C> {
    pub fn with_connector(endpoint: impl Into<String>, connector: C) -> Self {
        Self {
            endpoint: Some(endpoint.into()),
            connector,
            deadline: None,
            cancellation: None,
        }
    }
    pub fn installed_with_connector(connector: C) -> Self {
        Self {
            endpoint: None,
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
    /// Closed placement choices use the generated enum.
    ///
    /// ```compile_fail
    /// use abstraction_facade_service::{Connector, Machine};
    /// fn typo<C: Connector>(machine: &Machine<C>) {
    ///     let _ = machine.resolve_service("abstraction.logging/sink@1", vec![], "locla");
    /// }
    /// ```
    pub fn resolve_service(
        &self,
        contract: &str,
        guarantees: Vec<String>,
        scope: wire::Scope,
    ) -> Result<Binding<C>, Error<TransportError<C>>> {
        if !distinct(&guarantees) || contract.is_empty() {
            return Err(Error::InvalidRequirements);
        }
        let deadline = self
            .deadline
            .unwrap_or_else(|| Instant::now() + Duration::from_secs(5));
        let request = wire::ResolveRequest {
            capability: contract.split('/').next().unwrap_or("").into(),
            contracts: vec![contract.into()],
            guarantees,
            scope,
        };
        let refused = |failure, looked_for: &str, cause| {
            Error::Resolution(ResolutionError {
                failure,
                capability: request.capability.clone(),
                contract: contract.into(),
                looked_for: looked_for.into(),
                cause,
                platform: None,
            })
        };
        if self.endpoint.is_none() {
            if let Some(platform) = unsupported_platform(self.connector.platform()) {
                return Err(Error::Resolution(ResolutionError {
                    failure: ResolutionFailure::RuntimeUnavailable,
                    capability: request.capability.clone(),
                    contract: contract.into(),
                    looked_for: "the installed runtime".into(),
                    cause: None,
                    platform: Some(platform),
                }));
            }
        }
        let (connector, endpoint, looked_for) = match &self.endpoint {
            Some(endpoint) => (
                self.connector.clone(),
                endpoint.clone(),
                format!("the explicit endpoint {endpoint}"),
            ),
            None => {
                // Selection first: the resolver and the provider it names are
                // reached only through the identity the installation declares.
                let selected = self
                    .connector
                    .select_installed(deadline, self.cancellation.clone())
                    .and_then(|c| c.runtime_endpoint().map(|endpoint| (c, endpoint)));
                match selected {
                    Ok((connector, endpoint)) => {
                        let looked_for = format!("the installed runtime at {endpoint}");
                        (connector, endpoint, looked_for)
                    }
                    Err(e) if self.connector.is_cancellation(&e) => {
                        return Err(Error::Transport(e))
                    }
                    Err(e) => {
                        return Err(refused(
                            ResolutionFailure::RuntimeUnavailable,
                            "the installed runtime",
                            Some(e),
                        ))
                    }
                }
            }
        };
        let t = match connector.connect_resolver(
            &endpoint,
            deadline,
            self.cancellation.clone(),
            1024 * 1024,
        ) {
            Ok(t) => t,
            Err(e) if self.connector.is_cancellation(&e) => return Err(Error::Transport(e)),
            Err(e) => {
                return Err(refused(
                    ResolutionFailure::RuntimeUnavailable,
                    &looked_for,
                    Some(e),
                ))
            }
        };
        let result = match wire::ResolverClient::new(t).resolve(request.clone()) {
            Ok(result) => result,
            Err(wire::CallError::Transport(e)) if !connector.is_cancellation(&e) => {
                return Err(refused(
                    ResolutionFailure::RuntimeUnavailable,
                    &looked_for,
                    Some(e),
                ))
            }
            Err(e) => return Err(Error::Call(e)),
        };
        let reference = validate_binding(&request, result)
            .map_err(|failure| refused(failure, &looked_for, None))?;
        if !connector.supports(reference.scope, &reference.transport) {
            let failure = ResolutionFailure::UnsupportedTransport {
                scope: reference.scope,
                transport: reference.transport,
            };
            return Err(refused(failure, &looked_for, None));
        }
        let mut binding = Binding::restore(
            connector,
            &reference.endpoint,
            self.deadline,
            self.cancellation.clone(),
            1024 * 1024,
        )
        .map_err(Error::Transport)?;
        binding.reference = Some(std::sync::Arc::new(reference));
        Ok(binding)
    }
    /// Binds the runtime's provider registry, `abstraction.facade/registry@1`:
    /// the declarations a person placed in the runtime and its reading of each.
    /// An operator tool; the runtime decides `provider.manage` on every call.
    pub fn resolve_registry(
        &self,
        guarantees: Vec<String>,
        scope: wire::Scope,
    ) -> Result<wire::RegistryClient<Binding<C>>, Error<TransportError<C>>> {
        Ok(wire::RegistryClient::new(self.resolve_service(
            "abstraction.facade/registry@1",
            guarantees,
            scope,
        )?))
    }
    /// Calls `abstraction.facade/endpoint@1` `Describe` on one local endpoint:
    /// the services it hosts and each one's readiness. The description's
    /// program is the provider's own claim; the connector binds the server.
    pub fn describe_endpoint(
        &self,
        endpoint: &str,
    ) -> Result<wire::Description, Error<TransportError<C>>> {
        use wire::Endpoint;
        let binding = Binding::restore(
            self.connector.clone(),
            endpoint,
            self.deadline,
            self.cancellation.clone(),
            1 << 20,
        )
        .map_err(Error::Transport)?;
        wire::EndpointClient::new(binding)
            .describe()
            .map_err(Error::Call)
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
fn validate_binding(
    request: &wire::ResolveRequest,
    result: wire::ResolveResult,
) -> Result<wire::ServiceReference, ResolutionFailure> {
    if result.status != wire::ResolutionStatus::Resolved {
        if result.reference.is_some() {
            return Err(ResolutionFailure::InvalidResolution);
        }
        return Err(ResolutionFailure::Refused(result.status));
    }
    let r = result
        .reference
        .ok_or(ResolutionFailure::InvalidResolution)?;
    if r.provider.is_empty()
        || r.endpoint.is_empty()
        || r.endpoint.contains('\0')
        || r.capability != request.capability
        || !request.contracts.contains(&r.contract)
        || !matches!(r.scope, wire::Scope::Local | wire::Scope::Remote)
        || (request.scope != wire::Scope::Any && request.scope != r.scope)
        || !distinct(&r.guarantees)
        || !request.guarantees.iter().all(|g| r.guarantees.contains(g))
    {
        return Err(ResolutionFailure::InvalidResolution);
    }
    Ok(r)
}

#[cfg(test)]
mod tests;
