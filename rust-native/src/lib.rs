//! Installed shared-IPC connector for the transport-independent facade.
use abstraction_facade_service::Connector;
pub use abstraction_facade_service::wire::Scope;
pub use abstraction_ipc::{Cancellation, Error as TransportError, ServerExpectation};
use std::time::{Duration, Instant};
/// Connects through the shared C ABI. A connector holding a server expectation
/// verifies that identity on every connection before any byte is sent; the
/// default connector holds none and serves explicit endpoints unverified, as the
/// other languages' explicit endpoints do.
#[derive(Clone, Default)]
pub struct NativeConnector {
    server: Option<ServerExpectation>,
}
impl NativeConnector {
    /// A connector that requires `server` on every connection it opens.
    pub fn verified(server: ServerExpectation) -> Self {
        Self {
            server: Some(server),
        }
    }
    /// The identity every connection must prove, when one is required.
    pub fn server(&self) -> Option<&ServerExpectation> {
        self.server.as_ref()
    }
}
impl Connector for NativeConnector {
    type Transport = abstraction_ipc::FrameTransport;
    type Cancellation = Cancellation;
    fn supports(&self, scope: abstraction_facade_service::wire::Scope, transport: &str) -> bool {
        matches!(scope, abstraction_facade_service::wire::Scope::Local | abstraction_facade_service::wire::Scope::Remote)
            && transport == "oa-framed-local@1"
    }
    fn connect(
        &self,
        endpoint: &str,
        deadline: Instant,
        cancellation: Option<Cancellation>,
        max_frame: usize,
    ) -> Result<Self::Transport, TransportError> {
        let mut t = abstraction_ipc::FrameTransport::new(endpoint, Duration::from_secs(5))?
            .with_deadline(deadline)
            .with_limit(max_frame)?;
        if let Some(server) = &self.server {
            t = t.with_server_expectation(server.clone())?;
        }
        if let Some(c) = cancellation {
            t = t.with_cancellation(c);
        }
        Ok(t)
    }
    /// The resolver endpoint serves sessions: its connection is kept for the
    /// next resolution by the shared library's pool.
    fn connect_resolver(
        &self,
        endpoint: &str,
        deadline: Instant,
        cancellation: Option<Cancellation>,
        max_frame: usize,
    ) -> Result<Self::Transport, TransportError> {
        Ok(self
            .connect(endpoint, deadline, cancellation, max_frame)?
            .with_sessions(true))
    }
    fn runtime_endpoint(&self) -> Result<String, TransportError> {
        abstraction_ipc::runtime_endpoint()
    }
    /// Selects the installed runtime through the shared native selector, the one
    /// the Go, C++ and Python defaults use. A connector already holding an
    /// explicit expectation keeps it.
    fn select_installed(
        &self,
        deadline: Instant,
        cancellation: Option<Cancellation>,
    ) -> Result<Self, TransportError> {
        if self.server.is_some() {
            return Ok(self.clone());
        }
        abstraction_ipc::select_runtime(deadline, cancellation.as_ref()).map(Self::verified)
    }
    fn is_cancellation(&self, error: &TransportError) -> bool {
        error.status == abstraction_ipc::CANCELLED
    }
}
pub type Machine = abstraction_facade_service::Machine<NativeConnector>;
/// Resolves through the installed runtime. Each resolution selects the
/// runtime's identity, reads its endpoint and verifies that identity on the
/// resolver and provider connections, so a missing, ambiguous or impostor
/// runtime is that resolution's `runtime_unavailable`.
pub fn discover() -> Machine {
    Machine::installed()
}
