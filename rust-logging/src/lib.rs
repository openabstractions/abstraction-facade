//! Optional typed logging access through shared resolution.
use abstraction_facade_service::{Binding, Connector, Error, Machine, TransportError};
pub use abstraction_logging_api as logging;
mod async_writer;
pub use async_writer::{
    instant, AsyncOptions, AsyncWriter, CountsCallback, Counts, Deliver, DeliveryError, FailureCallback, GAP_MESSAGE,
};

/// The sink `resolve_log` returns, owned by an `AsyncWriter` and rebound through
/// its machine when the service disappears [LOG-S12].
pub struct ResolvedSink<C: Connector> {
    machine: Machine<C>,
    guarantees: Vec<String>,
    scope: abstraction_facade_service::wire::Scope,
    client: logging::SinkClient<Binding<C>>,
}

impl<C: Connector> ResolvedSink<C>
where
    TransportError<C>: std::fmt::Debug,
{
    /// Resolves `abstraction.logging/sink@1` now. An absent service is the
    /// facade's resolution error, and no sink is made [LOG-S11].
    pub fn resolve(machine: Machine<C>, guarantees: Vec<String>, scope: abstraction_facade_service::wire::Scope) -> Result<Self, Error<TransportError<C>>> {
        let client = machine.resolve_log(guarantees.clone(), scope)?;
        Ok(Self { machine, guarantees, scope, client })
    }
}

impl<C> Deliver for ResolvedSink<C>
where
    C: Connector + Send + 'static,
    C::Transport: Send,
    C::Cancellation: Send,
    TransportError<C>: std::fmt::Debug,
{
    fn deliver(&mut self, record: &logging::Record) -> Result<(), DeliveryError> {
        use logging::Sink;
        self.client.write(record.clone()).map_err(|error| DeliveryError::write_failed(format!("{error:?}")))
    }

    fn rebind(&mut self) -> Result<(), DeliveryError> {
        match self.machine.resolve_log(self.guarantees.clone(), self.scope) {
            Ok(client) => {
                self.client = client;
                Ok(())
            }
            Err(Error::Resolution(refused)) => Err(DeliveryError::resolution(refused.failure.status(), refused.to_string())),
            Err(other) => Err(DeliveryError::write_failed(other.to_string())),
        }
    }
}
pub trait LoggingMachine<C: Connector> {
    fn resolve_log(
        &self,
        g: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<logging::SinkClient<Binding<C>>, Error<TransportError<C>>>;
    fn resolve_log_reader(
        &self,
        g: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<History<Binding<C>>, Error<TransportError<C>>>;
    /// Long-poll observation; the history policy decides every call.
    fn resolve_log_observer(
        &self,
        g: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Observer<Binding<C>>, Error<TransportError<C>>>;
}
impl<C: Connector> LoggingMachine<C> for Machine<C> {
    fn resolve_log_observer(
        &self,
        g: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<Observer<Binding<C>>, Error<TransportError<C>>> {
        Ok(Observer(logging::HistoryObserverClient::new(
            self.resolve_service("abstraction.logging/observer@1", g, scope)?,
        )))
    }
    fn resolve_log(
        &self,
        g: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<logging::SinkClient<Binding<C>>, Error<TransportError<C>>> {
        Ok(logging::SinkClient::new(self.resolve_service(
            "abstraction.logging/sink@1",
            g,
            scope,
        )?))
    }
    fn resolve_log_reader(
        &self,
        g: Vec<String>,
        scope: abstraction_facade_service::wire::Scope,
    ) -> Result<History<Binding<C>>, Error<TransportError<C>>> {
        Ok(History(logging::HistoryReaderClient::new(
            self.resolve_service("abstraction.logging/reader@1", g, scope)?,
        )))
    }
}
pub struct History<T: logging::FrameTransport>(logging::HistoryReaderClient<T>);
impl<T: logging::FrameTransport> History<T> {
    pub fn read(
        &self,
        cursor: String,
        max_records: i64,
        max_bytes: i64,
    ) -> Result<logging::Page, logging::CallError<T::Error>> {
        use logging::HistoryReader;
        if !(1..=256).contains(&max_records) || !(1..=65536).contains(&max_bytes) {
            return Err(logging::CallError::Dispatch("invalid history limits"));
        }
        let page = self.0.read(cursor.clone(), max_records, max_bytes)?;
        validate_history::<T>(&page, &cursor, max_records, max_bytes)?;
        Ok(page)
    }
}

/// Long-poll history observation with the reader's bounds. At an empty current
/// end the service waits up to `wait_ms` (0..30000) for a new record. The
/// binding's waiting budget must cover `wait_ms`; calls are never retried.
/// Policy refusals arrive as service codes `forbidden` and `policy_unavailable`.
pub struct Observer<T: logging::FrameTransport>(logging::HistoryObserverClient<T>);
impl<T: logging::FrameTransport> Observer<T> {
    pub fn observe(
        &self,
        cursor: &str,
        max_records: i64,
        max_bytes: i64,
        wait_ms: i64,
    ) -> Result<logging::Page, logging::CallError<T::Error>> {
        use logging::HistoryObserver;
        if !(1..=256).contains(&max_records) || !(1..=65536).contains(&max_bytes) || !(0..=30000).contains(&wait_ms) {
            return Err(logging::CallError::Dispatch("invalid observation limits"));
        }
        let page = self.0.observe(cursor.into(), max_records, max_bytes, wait_ms)?;
        validate_observation::<T>(&page, cursor, max_records, max_bytes)?;
        Ok(page)
    }
}

/// Observation keeps the reader's page shape and adds `unsupported` for
/// providers without notification.
fn validate_observation<T: logging::FrameTransport>(
    page: &logging::Page,
    cursor: &str,
    max_records: i64,
    max_bytes: i64,
) -> Result<(), logging::CallError<T::Error>> {
    if page.outcome == "unsupported" {
        if !page.records.is_empty() || page.next != cursor || page.at_end {
            return Err(logging::CallError::Dispatch("invalid history refusal"));
        }
        return Ok(());
    }
    validate_history::<T>(page, cursor, max_records, max_bytes)
}

fn validate_history<T: logging::FrameTransport>(
    page: &logging::Page,
    cursor: &str,
    max_records: i64,
    max_bytes: i64,
) -> Result<(), logging::CallError<T::Error>> {
    if page.outcome == "page" {
        if page.records.len() > max_records as usize
            || page
                .records
                .iter()
                .map(|r| logging::encode(r).len())
                .sum::<usize>()
                > max_bytes as usize
            || page.next.is_empty()
            || (!page.records.is_empty() && page.next == cursor)
            || (page.records.is_empty() && !page.at_end)
        {
            return Err(logging::CallError::Dispatch("invalid history page"));
        }
    } else if !matches!(
        page.outcome.as_str(),
        "gap" | "unavailable" | "invalid_request" | "record_too_large" | "corrupt"
    ) || !page.records.is_empty()
        || page.next != cursor
        || page.at_end
    {
        return Err(logging::CallError::Dispatch("invalid history refusal"));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    struct Frames;
    impl logging::FrameTransport for Frames {
        type Error = ();
        fn write_frame(&self, _: &[u8]) -> Result<(), ()> {
            Ok(())
        }
        fn exchange_frame(&self, _: &[u8]) -> Result<Vec<u8>, ()> {
            Ok(vec![])
        }
    }
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

    /// VISION.md 2026-09-16: an adopted capability with no runtime fails with the
    /// facade's resolution error, visibly, and the application substitutes nothing.
    /// "A logging seam that quietly writes to stderr instead is the same defect"
    /// [LOG-S11]. `resolve_log` returns no sink, only the error.
    #[test]
    fn adopted_sink_with_no_runtime_is_the_resolution_error() {
        let machine = Machine::installed_with_connector(NoRuntime);
        let error = match machine.resolve_log(vec![], abstraction_facade_service::wire::Scope::Local) {
            Ok(_) => panic!("resolve_log produced a sink with no runtime"),
            Err(error) => error,
        };
        assert_eq!(
            error.to_string(),
            "service resolution: runtime_unavailable: abstraction.logging/sink@1 (capability abstraction.logging) at the installed runtime"
        );
        match error {
            Error::Resolution(r) => {
                assert_eq!(
                    r.failure,
                    abstraction_facade_service::ResolutionFailure::RuntimeUnavailable
                );
                assert_eq!(
                    (r.capability.as_str(), r.contract.as_str()),
                    ("abstraction.logging", "abstraction.logging/sink@1")
                );
                assert_eq!(r.cause, Some(Down("no runtime installed")));
            }
            other => panic!("want Error::Resolution, got {other:?}"),
        }
    }

    /// The asynchronous writer's sink resolves eagerly, so an absent runtime is
    /// the resolution error and no writer exists to queue records [LOG-S11].
    #[test]
    fn resolved_sink_with_no_runtime_is_the_resolution_error() {
        let machine = Machine::installed_with_connector(NoRuntime);
        match ResolvedSink::resolve(
            machine,
            vec![],
            abstraction_facade_service::wire::Scope::Local,
        ) {
            Ok(_) => panic!("a resolved sink exists with no runtime"),
            Err(Error::Resolution(r)) => assert_eq!(r.failure.status(), "runtime_unavailable"),
            Err(other) => panic!("want Error::Resolution, got {other:?}"),
        }
    }

    #[test]
    fn malformed_history_and_valid_end() {
        let mut page = logging::Page {
            outcome: logging::PageOutcome::Page,
            records: vec![],
            next: "cursor".into(),
            at_end: true,
        };
        assert!(validate_history::<Frames>(&page, "cursor", 1, 65536).is_ok());
        page.at_end = false;
        assert!(validate_history::<Frames>(&page, "cursor", 1, 65536).is_err());
        page.at_end = true;
        page.records.push(logging::Record {
            schema: 1,
            time: "2026-09-12T12:00:00.000000Z".into(),
            msg: "record".into(),
            ..Default::default()
        });
        assert!(validate_history::<Frames>(&page, "cursor", 1, 65536).is_err());
        page.next = "advanced".into();
        assert!(validate_history::<Frames>(&page, "cursor", 1, 65536).is_ok());
        assert!(validate_history::<Frames>(&page, "cursor", 1, 1).is_err());
        page.outcome = logging::PageOutcome::Gap;
        assert!(validate_history::<Frames>(&page, "cursor", 1, 65536).is_err());
        page.records.clear();
        page.next = "cursor".into();
        page.at_end = false;
        assert!(validate_history::<Frames>(&page, "cursor", 1, 65536).is_ok());
    }
    #[test]
    fn observation_accepts_unsupported_only_as_a_refusal() {
        let mut page = logging::Page {
            outcome: logging::PageOutcome::Unsupported,
            records: vec![],
            next: "cursor".into(),
            at_end: false,
        };
        assert!(validate_observation::<Frames>(&page, "cursor", 16, 65536).is_ok());
        assert!(validate_history::<Frames>(&page, "cursor", 16, 65536).is_err());
        page.next = "moved".into();
        assert!(validate_observation::<Frames>(&page, "cursor", 16, 65536).is_err());
        page.outcome = logging::PageOutcome::Page;
        page.at_end = true;
        assert!(validate_observation::<Frames>(&page, "cursor", 16, 65536).is_ok());
    }
}
