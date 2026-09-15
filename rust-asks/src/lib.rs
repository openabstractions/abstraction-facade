//! Resolved question admission and authorized operator clients.
use abstraction_facade_service::{Binding, Connector, Machine, TransportError};
pub use abstraction_asks_api as wire;
use wire::{QuestionApplication, QuestionOperator};

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

/// IDs, request keys and options are 1..128 bytes without control characters.
pub fn word(s: &str) -> bool {
    !s.is_empty() && s.len() <= 128 && !s.chars().any(char::is_control)
}

/// A pending record carries no decision; an answered record names one of its options.
pub fn valid_record(r: &wire::RecordMetadata) -> bool {
    word(&r.id)
        && !r.text.is_empty()
        && !r.options.is_empty()
        && r.option.is_empty() == r.answered.is_empty()
        && (r.option.is_empty() || r.options.contains(&r.option))
        && (!r.option.is_empty() || (!r.yes && !r.kept))
}

pub fn check_observation<E>(r: &wire::QuestionObservation) -> Result<(), Error<E>> {
    require(
        matches!(r.outcome.as_str(), "pending" | "answered") == r.answer.is_some(),
        "observation answer",
    )
}

pub fn check_page<E>(r: &wire::OperatorPage, limit: i64) -> Result<(), Error<E>> {
    if r.outcome == "page" {
        require(
            r.records.len() as i64 <= limit
                && r.complete == r.next.is_empty()
                && r.records.iter().all(valid_record),
            "operator page",
        )
    } else {
        require(
            r.records.is_empty() && r.next.is_empty() && !r.complete,
            "operator page refusal",
        )
    }
}

pub fn check_decision<E>(r: &wire::OperatorDecision, id: &str, option: &str) -> Result<(), Error<E>> {
    require((r.outcome == "answered") == r.record.is_some(), "decision record")?;
    if let Some(v) = &r.record {
        require(valid_record(v) && v.id == id && v.option == option, "decision record")?;
    }
    Ok(())
}

/// Retirement carries the record on the retiring call and none on replay.
pub fn check_retirement<E>(r: &wire::OperatorRetirement, id: &str) -> Result<(), Error<E>> {
    if let Some(v) = &r.record {
        require(r.outcome == "retired" && valid_record(v) && v.id == id, "retirement record")?;
    }
    Ok(())
}

/// Admits and observes questions in the caller's bound scope. No automatic retry.
#[derive(Clone)]
pub struct Questions<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Questions<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn ask(&self, q: wire::ApplicationQuestion) -> Result<wire::QuestionObservation, Error<T::Error>> {
        require(word(&q.request_key) && word(&q.key), "question key")?;
        let r = wire::QuestionApplicationClient::new(self.transport.clone())
            .Ask(q)
            .map_err(Error::Call)?;
        check_observation(&r)?;
        Ok(r)
    }
    /// `gone` reports a retired or forgotten question: neither approval nor refusal.
    pub fn observe(&self, request_key: &str, wait_ms: i64) -> Result<wire::QuestionObservation, Error<T::Error>> {
        require(word(request_key) && (0..=30000).contains(&wait_ms), "observation request")?;
        let r = wire::QuestionApplicationClient::new(self.transport.clone())
            .Observe(request_key.into(), wait_ms)
            .map_err(Error::Call)?;
        check_observation(&r)?;
        Ok(r)
    }
}

/// Lists, answers and retires questions. Every call is subject to the host's
/// operator policy; `forbidden` and `unavailable` carry no data.
#[derive(Clone)]
pub struct Operator<T> {
    transport: T,
}
impl<T: wire::FrameTransport + Clone> Operator<T> {
    pub fn new(transport: T) -> Self {
        Self { transport }
    }
    pub fn list(&self, cursor: &str, limit: i64) -> Result<wire::OperatorPage, Error<T::Error>> {
        require(cursor.len() <= 256 && (1..=64).contains(&limit), "history range")?;
        let r = wire::QuestionOperatorClient::new(self.transport.clone())
            .ListQuestions(cursor.into(), limit)
            .map_err(Error::Call)?;
        check_page(&r, limit)?;
        Ok(r)
    }
    pub fn answer(&self, id: &str, option: &str) -> Result<wire::OperatorDecision, Error<T::Error>> {
        require(word(id) && word(option), "answer")?;
        let r = wire::QuestionOperatorClient::new(self.transport.clone())
            .AnswerQuestion(id.into(), option.into())
            .map_err(Error::Call)?;
        check_decision(&r, id, option)?;
        Ok(r)
    }
    pub fn retire(&self, id: &str) -> Result<wire::OperatorRetirement, Error<T::Error>> {
        require(word(id), "retirement")?;
        let r = wire::QuestionOperatorClient::new(self.transport.clone())
            .RetireQuestion(id.into())
            .map_err(Error::Call)?;
        check_retirement(&r, id)?;
        Ok(r)
    }
}

pub trait AsksMachine<C: Connector> {
    fn resolve_asks(
        &self,
        guarantees: Vec<String>,
        scope: &str,
    ) -> Result<Questions<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
    /// Resolution grants no operator authority; the host's operator policy decides each call.
    fn resolve_asks_operator(
        &self,
        guarantees: Vec<String>,
        scope: &str,
    ) -> Result<Operator<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>>;
}
impl<C: Connector> AsksMachine<C> for Machine<C> {
    fn resolve_asks(
        &self,
        g: Vec<String>,
        s: &str,
    ) -> Result<Questions<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Questions::new(self.resolve_service("abstraction.asks/application@1", g, s)?))
    }
    fn resolve_asks_operator(
        &self,
        g: Vec<String>,
        s: &str,
    ) -> Result<Operator<Binding<C>>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Operator::new(self.resolve_service("abstraction.asks/operator@1", g, s)?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    fn record(id: &str, option: &str) -> wire::RecordMetadata {
        let mut r = wire::RecordMetadata::default();
        r.id = id.into();
        r.text = "Allow download?".into();
        r.options = vec!["once".into(), "refuse".into()];
        r.option = option.into();
        if !option.is_empty() {
            r.answered = "2026-09-15T00:00:00Z".into();
        }
        r
    }
    #[test]
    fn operator_results_keep_their_shapes() {
        let mut retired = wire::OperatorRetirement::default();
        retired.outcome = "retired".into();
        assert!(check_retirement::<()>(&retired, "q1").is_ok());
        retired.record = Some(record("q1", ""));
        assert!(check_retirement::<()>(&retired, "q1").is_ok());
        assert!(check_retirement::<()>(&retired, "q2").is_err());
        retired.outcome = "forbidden".into();
        assert!(check_retirement::<()>(&retired, "q1").is_err());

        let mut decision = wire::OperatorDecision::default();
        decision.outcome = "answered".into();
        assert!(check_decision::<()>(&decision, "q1", "once").is_err());
        decision.record = Some(record("q1", "once"));
        assert!(check_decision::<()>(&decision, "q1", "once").is_ok());
        assert!(check_decision::<()>(&decision, "q1", "refuse").is_err());
        decision.record = Some(record("q1", "never"));
        assert!(check_decision::<()>(&decision, "q1", "never").is_err());

        let mut page = wire::OperatorPage::default();
        page.outcome = "forbidden".into();
        assert!(check_page::<()>(&page, 16).is_ok());
        page.records.push(record("q1", ""));
        assert!(check_page::<()>(&page, 16).is_err());
        page.outcome = "page".into();
        page.complete = true;
        assert!(check_page::<()>(&page, 16).is_ok());
        assert!(check_page::<()>(&page, 0).is_err());
        assert!(!word("bad\nid") && !word("") && word("q1"));
    }
}
