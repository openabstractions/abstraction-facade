//! Optional typed resource table and leases access through shared resolution,
//! following abstraction-facade-logging's pattern for a per-capability accessor.
use abstraction_facade_service::{Binding, Connector, Error, Machine, TransportError};
pub use abstraction_resource_client as resource;

/// `resolve_resource_table`/`resolve_resource_leases` bind
/// `abstraction.resource/table@1` and `abstraction.resource/leases@1` through
/// the machine's shared resolution, the same way `resolve_registry` binds
/// `abstraction.facade/registry@1`.
pub trait ResourceMachine<C: Connector> {
    fn resolve_resource_table(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<resource::TableClient<Binding<C>>, Error<TransportError<C>>>;
    fn resolve_resource_leases(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<resource::LeasesClient<Binding<C>>, Error<TransportError<C>>>;
}
impl<C: Connector> ResourceMachine<C> for Machine<C> {
    fn resolve_resource_table(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<resource::TableClient<Binding<C>>, Error<TransportError<C>>> {
        Ok(resource::TableClient::new(self.resolve_service(
            "abstraction.resource/table@1",
            guarantees,
            scope,
        )?))
    }
    fn resolve_resource_leases(
        &self,
        guarantees: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<resource::LeasesClient<Binding<C>>, Error<TransportError<C>>> {
        Ok(resource::LeasesClient::new(self.resolve_service(
            "abstraction.resource/leases@1",
            guarantees,
            scope,
        )?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A transport failure with a message, so the resolution error has a source.
    #[derive(Debug, PartialEq)]
    struct Down(&'static str);
    impl std::fmt::Display for Down {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            f.write_str(self.0)
        }
    }
    impl std::error::Error for Down {}

    /// A machine with no OpenAbstractions runtime installed.
    #[derive(Clone)]
    struct NoRuntime;
    impl abstraction_facade_service::FrameTransport for NoRuntime {
        type Error = Down;
        fn write_frame(&self, _: &[u8]) -> Result<(), Down> {
            Err(Down("no runtime"))
        }
        fn exchange_frame(&self, _: &[u8]) -> Result<Vec<u8>, Down> {
            Err(Down("no runtime"))
        }
    }
    impl Connector for NoRuntime {
        type Transport = Self;
        type Cancellation = ();
        fn supports(&self, scope: abstraction_facade_service::wire::Scope, transport: &str) -> bool {
            scope == "local" && transport == "oa-framed-local@1"
        }
        fn connect(
            &self,
            _: &str,
            _: std::time::Instant,
            _: Option<()>,
            _: usize,
        ) -> Result<Self, Down> {
            Err(Down("no runtime"))
        }
        fn runtime_endpoint(&self) -> Result<String, Down> {
            Err(Down("no runtime installed"))
        }
    }

    /// An adopted capability with no runtime fails with the facade's resolution
    /// error, visibly, naming the contract this accessor asked for — the same
    /// shape abstraction-facade-logging proves for resolve_log.
    #[test]
    fn resolve_resource_table_with_no_runtime_is_the_resolution_error() {
        let machine = Machine::installed_with_connector(NoRuntime);
        let error = match machine.resolve_resource_table(vec![], abstraction_facade_service::wire::Scope::Local) {
            Ok(_) => panic!("resolve_resource_table produced a client with no runtime"),
            Err(error) => error,
        };
        match error {
            Error::Resolution(r) => {
                assert_eq!(
                    r.failure,
                    abstraction_facade_service::ResolutionFailure::RuntimeUnavailable
                );
                assert_eq!(
                    (r.capability.as_str(), r.contract.as_str()),
                    ("abstraction.resource", "abstraction.resource/table@1")
                );
                assert_eq!(r.cause, Some(Down("no runtime installed")));
            }
            other => panic!("want Error::Resolution, got {other:?}"),
        }
    }

    #[test]
    fn resolve_resource_leases_with_no_runtime_is_the_resolution_error() {
        let machine = Machine::installed_with_connector(NoRuntime);
        let error = match machine.resolve_resource_leases(vec![], abstraction_facade_service::wire::Scope::Local) {
            Ok(_) => panic!("resolve_resource_leases produced a client with no runtime"),
            Err(error) => error,
        };
        match error {
            Error::Resolution(r) => {
                assert_eq!(
                    (r.capability.as_str(), r.contract.as_str()),
                    ("abstraction.resource", "abstraction.resource/leases@1")
                );
            }
            other => panic!("want Error::Resolution, got {other:?}"),
        }
    }
}
