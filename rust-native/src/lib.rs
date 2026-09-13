//! Installed shared-IPC connector for the transport-independent facade.
use abstraction_facade_service::Connector;
pub use abstraction_ipc::{Cancellation, Error as TransportError};
use std::time::{Duration, Instant};
#[derive(Clone, Default)]
pub struct NativeConnector;
impl Connector for NativeConnector {
    type Transport = abstraction_ipc::FrameTransport;
    type Cancellation = Cancellation;
    fn supports(&self, scope: &str, transport: &str) -> bool {
        scope == "local" && transport == "oa-framed-local@1"
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
        if let Some(c) = cancellation {
            t = t.with_cancellation(c);
        }
        Ok(t)
    }
}
pub type Machine = abstraction_facade_service::Machine<NativeConnector>;
pub fn discover() -> Result<Machine, TransportError> {
    Ok(Machine::new(abstraction_ipc::runtime_endpoint()?))
}
