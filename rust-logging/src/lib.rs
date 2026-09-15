//! Optional typed logging access through shared resolution.
use abstraction_facade_service::{Binding, Connector, Error, Machine, TransportError};
pub use abstraction_logging_api as logging;
pub trait LoggingMachine<C: Connector> {
    fn resolve_log(
        &self,
        g: Vec<String>,
        scope: &str,
    ) -> Result<logging::SinkClient<Binding<C>>, Error<TransportError<C>>>;
    fn resolve_log_reader(
        &self,
        g: Vec<String>,
        scope: &str,
    ) -> Result<History<Binding<C>>, Error<TransportError<C>>>;
    /// Long-poll observation; the history policy decides every call.
    fn resolve_log_observer(
        &self,
        g: Vec<String>,
        scope: &str,
    ) -> Result<Observer<Binding<C>>, Error<TransportError<C>>>;
}
impl<C: Connector> LoggingMachine<C> for Machine<C> {
    fn resolve_log_observer(
        &self,
        g: Vec<String>,
        scope: &str,
    ) -> Result<Observer<Binding<C>>, Error<TransportError<C>>> {
        Ok(Observer(logging::HistoryObserverClient::new(
            self.resolve_service("abstraction.logging/observer@1", g, scope)?,
        )))
    }
    fn resolve_log(
        &self,
        g: Vec<String>,
        scope: &str,
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
        scope: &str,
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
        let page = self.0.Read(cursor.clone(), max_records, max_bytes)?;
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
        let page = self.0.Observe(cursor.into(), max_records, max_bytes, wait_ms)?;
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
    #[test]
    fn malformed_history_and_valid_end() {
        let mut page = logging::Page {
            outcome: "page".into(),
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
        page.outcome = "gap".into();
        assert!(validate_history::<Frames>(&page, "cursor", 1, 65536).is_err());
        page.records.clear();
        page.next = "cursor".into();
        page.at_end = false;
        assert!(validate_history::<Frames>(&page, "cursor", 1, 65536).is_ok());
    }
    #[test]
    fn observation_accepts_unsupported_only_as_a_refusal() {
        let mut page = logging::Page {
            outcome: "unsupported".into(),
            records: vec![],
            next: "cursor".into(),
            at_end: false,
        };
        assert!(validate_observation::<Frames>(&page, "cursor", 16, 65536).is_ok());
        assert!(validate_history::<Frames>(&page, "cursor", 16, 65536).is_err());
        page.next = "moved".into();
        assert!(validate_observation::<Frames>(&page, "cursor", 16, 65536).is_err());
        page.outcome = "page".into();
        page.at_end = true;
        assert!(validate_observation::<Frames>(&page, "cursor", 16, 65536).is_ok());
        page.outcome = "future".into();
        assert!(validate_observation::<Frames>(&page, "moved", 16, 65536).is_err());
    }
}
