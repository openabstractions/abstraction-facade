//! Resolved model calls: `complete()` and a native `stream()` iterator over
//! `abstraction.inference/chat@1`.
//!
//! The runtime picks the host, decides `abstraction.inference/complete` for the
//! bound caller and applies a named credential itself. `stream()` hides Start,
//! Observe and Cancel; dropping the iterator before the end delta cancels the
//! operation. Start is never retried.
use abstraction_facade_service::{Binding, Connector, Machine, TransportError};
pub use abstraction_inference_api as wire;
use std::collections::{BTreeMap, VecDeque};
use std::time::{Duration, Instant};
use wire::Chat as _;

/// Page bounds `stream()` uses for each observe.
pub const PAGE_DELTAS: i64 = 256;
pub const PAGE_BYTES: i64 = 65536;
pub const PAGE_WAIT_MS: i64 = 25000;
const CALL_MARGIN: Duration = Duration::from_secs(5);

#[derive(Debug)]
pub enum Error<E> {
    Call(wire::CallError<E>),
    /// An observe outcome other than page.
    Observe(wire::PageOutcome),
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

/// A page holds consecutive deltas from cursor; a gap moves next forward; any
/// other refusal holds nothing and keeps the cursor.
pub fn check_page<E>(page: &wire::DeltaPage, cursor: i64, max_deltas: i64) -> Result<(), Error<E>> {
    match page.outcome {
        wire::PageOutcome::Page => require(
            page.deltas.len() as i64 <= max_deltas
                && page.next == cursor + page.deltas.len() as i64
                && page.deltas.iter().enumerate().all(|(i, d)| d.sequence == cursor + i as i64),
            "delta page",
        ),
        wire::PageOutcome::Gap => require(page.deltas.is_empty() && page.next > cursor, "gap"),
        _ => require(page.deltas.is_empty() && page.next == cursor && !page.at_end, "page refusal"),
    }
}

/// Assembles a reply: a part delta extends the part at its index, and the end
/// delta supplies outcome, reason, usage, host and model.
#[derive(Default)]
pub struct Fold {
    parts: BTreeMap<i64, wire::Part>,
    end: Option<wire::Reply>,
}
impl Fold {
    pub fn add(&mut self, delta: &wire::Delta) {
        match (delta.kind, &delta.part, &delta.end) {
            (wire::DeltaKind::Part, Some(d), _) => match self.parts.get_mut(&delta.index) {
                None => {
                    self.parts.insert(delta.index, d.clone());
                }
                Some(p) => {
                    p.text.push_str(&d.text);
                    p.arguments.push_str(&d.arguments);
                    for (into, from) in [
                        (&mut p.call_id, &d.call_id),
                        (&mut p.name, &d.name),
                        (&mut p.digest, &d.digest),
                        (&mut p.media_type, &d.media_type),
                    ] {
                        if into.is_empty() {
                            into.clone_from(from);
                        }
                    }
                }
            },
            (wire::DeltaKind::End, _, Some(end)) => self.end = Some(end.clone()),
            _ => {}
        }
    }
    /// The folded reply, once the end delta arrived.
    pub fn reply(&self) -> Option<wire::Reply> {
        let mut reply = self.end.clone()?;
        reply.message = wire::Message { role: wire::Role::Assistant, parts: self.parts.values().cloned().collect() };
        Some(reply)
    }
}

fn refused(admission: &wire::Admission) -> wire::Reply {
    wire::Reply {
        outcome: wire::ReplyOutcome::from_wire(admission.outcome.as_str()).unwrap_or(wire::ReplyOutcome::Invalid),
        reason: admission.reason.clone(),
        message: wire::Message { role: wire::Role::Assistant, parts: vec![] },
        stop_reason: wire::StopReason::NoStop,
        usage: wire::Usage::default(),
        host: String::new(),
        model: String::new(),
        cost: None,
    }
}

/// Model calls performed by the runtime for this program.
pub struct Chat<C: Connector> {
    binding: Binding<C>,
}
type Failure<C> = Error<TransportError<C>>;
impl<C: Connector> Chat<C> {
    pub fn new(binding: Binding<C>) -> Self {
        Self { binding }
    }
    fn client(&self, wait_ms: i64) -> Result<wire::ChatClient<Binding<C>>, Failure<C>> {
        let deadline = Instant::now() + CALL_MARGIN + Duration::from_millis(wait_ms.max(0) as u64);
        let binding = self.binding.with_waiting(Some(deadline), None).map_err(|e| Error::Call(wire::CallError::Transport(e)))?;
        Ok(wire::ChatClient::new(binding))
    }
    pub fn start(&self, request: wire::Request) -> Result<wire::Admission, Failure<C>> {
        let a = self.client(0)?.start(request).map_err(Error::Call)?;
        require((a.outcome == wire::StartOutcome::Accepted) == !a.operation.is_empty(), "admission")?;
        Ok(a)
    }
    /// The call's budget is the transport margin plus `wait_ms`.
    pub fn observe(&self, operation: &str, cursor: i64, max_deltas: i64, max_bytes: i64, wait_ms: i64) -> Result<wire::DeltaPage, Failure<C>> {
        require((1..=256).contains(&max_deltas) && (1..=65536).contains(&max_bytes) && (0..=30000).contains(&wait_ms), "page bounds")?;
        let page = self.client(wait_ms)?.observe(operation.into(), cursor, max_deltas, max_bytes, wait_ms).map_err(Error::Call)?;
        check_page(&page, cursor, max_deltas)?;
        Ok(page)
    }
    pub fn cancel(&self, operation: &str) -> Result<wire::Cancellation, Failure<C>> {
        self.client(0)?.cancel(operation.into()).map_err(Error::Call)
    }
    /// Deltas until the end delta, which carries the reply. A start refusal is
    /// one end delta carrying that outcome.
    pub fn stream(&self, request: wire::Request) -> Stream<'_, C> {
        let mut stream = Stream { chat: self, operation: String::new(), cursor: 0, pending: VecDeque::new(), ended: false };
        match self.start(request) {
            Err(e) => {
                stream.ended = true;
                stream.pending.push_back(Err(e));
            }
            Ok(a) if a.outcome != wire::StartOutcome::Accepted => {
                stream.ended = true;
                let end = refused(&a);
                stream.pending.push_back(Ok(wire::Delta {
                    sequence: 0,
                    kind: wire::DeltaKind::End,
                    index: 0,
                    part: None,
                    usage: None,
                    segment: None,
                    audio: None,
                    end: Some(end),
                    transcription_end: None,
                    speech_end: None, transcript: None, live_end: None,
                    image_progress: None, image_result: None, image_end: None,
                }));
            }
            Ok(a) => stream.operation = a.operation,
        }
        stream
    }
    /// Start request, observe it to its end and fold the deltas into the reply.
    pub fn complete(&self, request: wire::Request) -> Result<wire::Reply, Failure<C>> {
        let mut fold = Fold::default();
        for delta in self.stream(request) {
            fold.add(&delta?);
        }
        fold.reply().ok_or(Error::Invalid("stream ended without an end delta"))
    }
}

/// The deltas of one operation. Dropping it before the end delta cancels.
pub struct Stream<'a, C: Connector> {
    chat: &'a Chat<C>,
    operation: String,
    cursor: i64,
    pending: VecDeque<Result<wire::Delta, Failure<C>>>,
    ended: bool,
}
impl<C: Connector> Iterator for Stream<'_, C> {
    type Item = Result<wire::Delta, Failure<C>>;
    fn next(&mut self) -> Option<Self::Item> {
        loop {
            if let Some(item) = self.pending.pop_front() {
                return Some(item);
            }
            if self.ended {
                return None;
            }
            match self.chat.observe(&self.operation, self.cursor, PAGE_DELTAS, PAGE_BYTES, PAGE_WAIT_MS) {
                Err(e) => {
                    self.ended = true;
                    self.cancel_quietly();
                    return Some(Err(e));
                }
                Ok(page) if page.outcome != wire::PageOutcome::Page => {
                    self.ended = true;
                    self.cancel_quietly();
                    return Some(Err(Error::Observe(page.outcome)));
                }
                Ok(page) => {
                    self.cursor = page.next;
                    self.ended = page.at_end || page.deltas.iter().any(|d| d.kind == wire::DeltaKind::End);
                    self.pending.extend(page.deltas.into_iter().map(Ok));
                }
            }
        }
    }
}
impl<C: Connector> Stream<'_, C> {
    fn cancel_quietly(&mut self) {
        if !self.operation.is_empty() {
            // An operation whose cancel cannot be sent idles out at the service.
            let _ = self.chat.cancel(&self.operation);
            self.operation.clear();
        }
    }
}
impl<C: Connector> Drop for Stream<'_, C> {
    fn drop(&mut self) {
        if !self.ended {
            self.cancel_quietly();
        }
    }
}

pub trait InferenceMachine<C: Connector> {
    /// Resolution grants nothing; every start is a rights decision on the bound caller.
    fn resolve_inference(&self, guarantees: Vec<String>, scope: abstraction_facade_service::wire::Scope) -> Result<Chat<C>, abstraction_facade_service::Error<TransportError<C>>>;
}
impl<C: Connector> InferenceMachine<C> for Machine<C> {
    fn resolve_inference(&self, g: Vec<String>, s: abstraction_facade_service::wire::Scope) -> Result<Chat<C>, abstraction_facade_service::Error<TransportError<C>>> {
        Ok(Chat::new(self.resolve_service("abstraction.inference/chat@1", g, s)?))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn part(index: i64, kind: wire::PartKind, text: &str, call_id: &str, arguments: &str) -> wire::Delta {
        wire::Delta {
            sequence: index,
            kind: wire::DeltaKind::Part,
            index,
            part: Some(wire::Part {
                kind,
                text: text.into(),
                digest: String::new(),
                media_type: String::new(),
                call_id: call_id.into(),
                name: String::new(),
                arguments: arguments.into(),
            }),
            usage: None,
            segment: None,
            audio: None,
            end: None,
            transcription_end: None,
            speech_end: None, transcript: None, live_end: None,
            image_progress: None, image_result: None, image_end: None,
        }
    }

    #[test]
    fn fold_extends_parts_by_index() {
        let mut fold = Fold::default();
        assert!(fold.reply().is_none());
        fold.add(&part(0, wire::PartKind::Text, "Hel", "", ""));
        fold.add(&part(1, wire::PartKind::ToolCall, "", "call-1", "{\"a\""));
        fold.add(&part(0, wire::PartKind::Text, "lo", "", ""));
        fold.add(&part(1, wire::PartKind::ToolCall, "", "", ":1}"));
        let admission = wire::Admission {
            outcome: wire::StartOutcome::NotPermitted,
            reason: "rights:not_granted".into(),
            operation: String::new(),
            host: String::new(),
            model: String::new(),
            retention_ms: 0,
            idle_ms: 0,
            retained_deltas: 0,
        };
        let mut end = refused(&admission);
        assert_eq!(end.outcome, wire::ReplyOutcome::NotPermitted);
        end.outcome = wire::ReplyOutcome::Completed;
        fold.add(&wire::Delta { sequence: 4, kind: wire::DeltaKind::End, index: 0, part: None, usage: None, segment: None, audio: None, end: Some(end), transcription_end: None, speech_end: None, transcript: None, live_end: None, image_progress: None, image_result: None, image_end: None });
        let reply = fold.reply().expect("reply");
        assert_eq!(reply.message.parts.len(), 2);
        assert_eq!(reply.message.parts[0].text, "Hello");
        assert_eq!(reply.message.parts[1].arguments, "{\"a\":1}");
        assert_eq!(reply.message.parts[1].call_id, "call-1");
    }

    #[test]
    fn pages_keep_their_shapes() {
        let page = |outcome, next, at_end| wire::DeltaPage { outcome, deltas: vec![], next, at_end };
        assert!(check_page::<()>(&page(wire::PageOutcome::Page, 3, false), 3, 8).is_ok());
        assert!(check_page::<()>(&page(wire::PageOutcome::Page, 4, false), 3, 8).is_err());
        assert!(check_page::<()>(&page(wire::PageOutcome::Gap, 7, false), 3, 8).is_ok());
        assert!(check_page::<()>(&page(wire::PageOutcome::Unknown, 3, false), 3, 8).is_ok());
        assert!(check_page::<()>(&page(wire::PageOutcome::Unknown, 3, true), 3, 8).is_err());
    }
}
