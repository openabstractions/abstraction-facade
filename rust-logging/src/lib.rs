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
}
impl<C: Connector> LoggingMachine<C> for Machine<C> {
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
}
